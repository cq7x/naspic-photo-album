package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm/clause"

	"github.com/naspic/naspic/internal/cache"
	"github.com/naspic/naspic/internal/media"
	"github.com/naspic/naspic/internal/model"
	"github.com/naspic/naspic/internal/storage"
)

// uploadWeb 网页端多文件上传（multipart/form-data，字段名 files）。
//
// 约束：
//  1. 只有可写的托管库能上传；挂载只读库直接 403（绝不往宿主机目录写东西）；
//  2. 同名文件自动重命名，绝不覆盖；
//  3. 相同哈希视为重复，直接复用已有记录（秒传）。
func (s *Server) uploadWeb(c *gin.Context) {
	libID, _ := strconv.ParseInt(c.PostForm("library_id"), 10, 64)
	if libID <= 0 {
		fail(c, http.StatusBadRequest, "缺少 library_id")
		return
	}
	d, exist := s.drivers.Get(libID)
	if !exist {
		fail(c, 404, "相册库不存在或未就绪")
		return
	}
	if !d.Writable() {
		fail(c, http.StatusForbidden, "该库为只读（挂载目录），请改用可写的托管库上传")
		return
	}

	form, err := c.MultipartForm()
	if err != nil {
		fail(c, http.StatusBadRequest, "解析上传失败: "+err.Error())
		return
	}
	files := form.File["files"]
	if len(files) == 0 {
		fail(c, http.StatusBadRequest, "没有选择文件")
		return
	}
	if len(files) > 200 {
		fail(c, http.StatusBadRequest, "单次最多上传 200 个文件")
		return
	}

	// 目标子目录：默认按上传月份归档 2026/09
	subDir := strings.Trim(strings.ReplaceAll(c.PostForm("dir"), `\`, "/"), "/")
	subDir = strings.Trim(subDir, ".")
	if subDir == "" {
		subDir = time.Now().Format("2006/01")
	}

	type result struct {
		Name    string `json:"name"`
		RelPath string `json:"rel_path,omitempty"`
		MediaID int64  `json:"media_id,omitempty"`
		Dedup   bool   `json:"dedup,omitempty"`
		Renamed bool   `json:"renamed,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	out := make([]result, 0, len(files))
	added := 0

	for _, fh := range files {
		r := result{Name: fh.Filename}
		name := sanitizeName(filepath.Base(fh.Filename))
		if name == "" {
			r.Error = "文件名为空"
			out = append(out, r)
			continue
		}
		// 扩展名统一为「不含点」的小写形式，与 storage.ExtOf / scanner 保持一致
		ext := storage.ExtOf(name)
		if !storage.IsSupportedExt(ext) {
			r.Error = "不支持的格式 ." + ext
			out = append(out, r)
			continue
		}

		// 先落临时文件（便于算哈希与解析 EXIF，避免二次读流）
		tmp := filepath.Join(os.TempDir(),
			"naspic-up-"+strconv.FormatInt(time.Now().UnixNano(), 10)+"."+ext)
		src, err := fh.Open()
		if err != nil {
			r.Error = "读取失败: " + err.Error()
			out = append(out, r)
			continue
		}
		dst, err := os.Create(tmp)
		if err != nil {
			src.Close()
			r.Error = "临时文件创建失败"
			out = append(out, r)
			continue
		}
		// 落临时文件 + 同程算哈希（一次读写搞定，不再落盘后又整文件读一遍）
		hasher := sha256.New()
		if _, err := io.Copy(dst, io.TeeReader(src, hasher)); err != nil {
			src.Close()
			dst.Close()
			os.Remove(tmp)
			r.Error = "写入临时文件失败"
			out = append(out, r)
			continue
		}
		src.Close()
		dst.Close()
		hash := hex.EncodeToString(hasher.Sum(nil))

		// 秒传：同库已存在相同哈希
		// 用 Find+Limit 而不是 First：查不到是常态，不该在日志里刷 record not found
		var dups []model.MediaFile
		s.db.Where("library_id = ? AND hash = ? AND deleted_at IS NULL", libID, hash).
			Limit(1).Find(&dups)
		if len(dups) > 0 {
			dup := dups[0]
			os.Remove(tmp)
			r.Dedup = true
			r.MediaID = dup.ID
			r.RelPath = dup.RelativePath
			out = append(out, r)
			continue
		}

		fp, err := os.Open(tmp)
		if err != nil {
			os.Remove(tmp)
			r.Error = "读取临时文件失败"
			out = append(out, r)
			continue
		}
		ctype := fh.Header.Get("Content-Type")
		// 浏览器偶尔会甩来 application/octet-stream，直链播放会变成下载，按扩展名兜底
		if ctype == "" || ctype == "application/octet-stream" {
			ctype = mimeByExt(ext)
		}
		up, err := d.UploadFile(c, fp, storage.UploadOption{
			RelPath:   subDir + "/" + name,
			Size:      fh.Size,
			Mime:      ctype,
			Mtime:     time.Now(),
			Overwrite: false, // 同名自动重命名，绝不覆盖
		})
		fp.Close()
		if err != nil {
			os.Remove(tmp)
			r.Error = err.Error()
			out = append(out, r)
			continue
		}

		// 元数据：EXIF 优先，失败用 mtime 兜底
		taken := time.Now()
		takeSrc := int8(2)
		var mk, mdl string
		var lat, lon *float64
		var orient int16
		var exifJSON string
		var w, h int
		var durMs int
		if abs, jerr := storage.SafeJoin(d.Root(), up.RelPath); jerr == nil {
			if mt, merr := media.Parse(abs); merr == nil && mt != nil {
				if mt.TakenAt != nil {
					taken = *mt.TakenAt
				}
				takeSrc = mt.TakenSource
				mk, mdl = mt.Make, mt.Model
				lat, lon = mt.Lat, mt.Lon
				orient = mt.Orientation
				exifJSON = mt.ExifJSON
				w, h = mt.Width, mt.Height
				durMs = mt.DurationMs
			}
		}

		now := time.Now()
		rec := model.MediaFile{
			LibraryID: libID, SourceType: model.SourceManaged,
			RelativePath: up.RelPath, Filename: filepath.Base(up.RelPath),
			Ext: ext, Mime: ctype, SizeBytes: fh.Size,
			Width: w, Height: h, DurationMs: durMs,
			Hash: hash, HashAlgo: "sha256",
			MediaType: storage.MediaTypeOf(ext),
			TakenAt: &taken, TakenAtSource: takeSrc,
			CameraMake: mk, CameraModel: mdl,
			GPSLat: lat, GPSLon: lon, Orientation: orient, ExifJSON: exifJSON,
			IndexStatus: model.IndexNormal,
			CreatedAt:   &now, UpdatedAt: &now,
		}
		if err := s.db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "library_id"}, {Name: "relative_path"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"size_bytes", "hash", "taken_at", "mime", "exif_json", "width", "height",
				"duration_ms", "updated_at", "deleted_at",
			}),
		}).Create(&rec).Error; err != nil {
			os.Remove(tmp)
			r.Error = "索引写入失败: " + err.Error()
			out = append(out, r)
			continue
		}
		// OnConflict 的 assignment 里放 deleted_at 是为了重新上传时复活记录，这里显式清零
		s.db.Model(&model.MediaFile{}).Where("id = ?", rec.ID).Update("deleted_at", nil)
		go prewarmThumb(d, up.RelPath)

		os.Remove(tmp)
		added++
		r.MediaID = rec.ID
		r.RelPath = up.RelPath
		r.Renamed = up.Renamed
		out = append(out, r)
	}

	if added > 0 {
		s.db.Exec("UPDATE libraries SET media_count = (SELECT COUNT(*) FROM media_files WHERE library_id = ? AND deleted_at IS NULL) WHERE id = ?", libID, libID)
	}
	if added > 0 {
		// 新照片进来了：列表/统计/库列表的缓存全部作废
		s.invalidate(cache.PfxMedia)
		s.invalidate(cache.PfxLibrary)
	}
	ok(c, gin.H{"uploaded": added, "results": out})
}

// prewarmSem 限制「上传后预热缩略图」的并发，避免一次传 100 张把 CPU 打满
var prewarmSem = make(chan struct{}, 2)

// prewarmThumb 后台提前生成缩略图，前端刷新列表时就能直接命中缓存。
// 队列忙则直接放弃——前端浏览时会走懒加载，不会漏。
func prewarmThumb(d storage.StorageDriver, key string) {
	select {
	case prewarmSem <- struct{}{}:
	default:
		return
	}
	defer func() { <-prewarmSem }()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, _, _ = d.GetThumb(ctx, key, storage.ThumbSM)
}

// sanitizeName 清洗文件名：去路径分隔符与控制字符，避免穿越与非法名
func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if r < 32 || r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if len([]rune(out)) > 180 {
		out = string([]rune(out)[:180])
	}
	return out
}

// mimeByExt 兜底 Content-Type（历史数据 Mime 可能为空）
func mimeByExt(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "heic":
		return "image/heic"
	case "heif":
		return "image/heif"
	case "bmp":
		return "image/bmp"
	case "tif", "tiff":
		return "image/tiff"
	case "mp4":
		return "video/mp4"
	case "mov":
		return "video/quicktime"
	case "avi":
		return "video/x-msvideo"
	case "mkv":
		return "video/x-matroska"
	case "webm":
		return "video/webm"
	}
	return "application/octet-stream"
}
