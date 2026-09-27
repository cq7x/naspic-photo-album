package api

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gabriel-vasile/mimetype"

	"github.com/naspic/naspic/internal/cache"
	"github.com/naspic/naspic/internal/media"
	"github.com/naspic/naspic/internal/model"
	"github.com/naspic/naspic/internal/storage"
)

// 上传三步：check（秒传探测）→ init（建会话）→ chunk（并发分片）→ complete（合并校验入库）

type uploadCheckReq struct {
	LibraryID int64  `json:"library_id"`
	DeviceID  int64  `json:"device_id"`
	Hash      string `json:"hash"`
	Size      int64  `json:"size"`
	Filename  string `json:"filename"`
}

// uploadCheck 秒传探测：服务端已存在同哈希文件则直接关联，不再传输
func (s *Server) uploadCheck(c *gin.Context) {
	var req uploadCheckReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if req.Hash == "" {
		fail(c, http.StatusBadRequest, "hash 不能为空")
		return
	}
	// library_id=0 时尝试自动解析/创建以设备名命名的托管库
	if req.LibraryID == 0 && req.DeviceID > 0 {
		if libID, err := s.ensureDeviceLibrary(c, req.DeviceID); err == nil {
			req.LibraryID = libID
		}
	}
	var m model.MediaFile
	err := s.db.Where("hash = ? AND deleted_at IS NULL", req.Hash).
		Order("id DESC").First(&m).Error
	if err == nil {
		ok(c, gin.H{"exists": true, "media_id": m.ID, "library_id": req.LibraryID})
		return
	}
	// 哈希没匹配（例如旧版本用不同哈希算法存过、或 App 端采样哈希与全量不一致）：
	// 再用「库 + 文件名 + 大小」兜底判断，避免把已存在的文件又传一遍、最后撞唯一键 500。
	if req.Filename != "" {
		var m2 model.MediaFile
		if err2 := s.db.Where("library_id = ? AND filename = ? AND size_bytes = ? AND deleted_at IS NULL",
			req.LibraryID, req.Filename, req.Size).First(&m2).Error; err2 == nil {
			ok(c, gin.H{"exists": true, "media_id": m2.ID, "library_id": req.LibraryID})
			return
		}
	}

	// 查进行中的会话，支持断点续传
	var sess model.UploadSession
	if err := s.db.Where("file_hash = ? AND size_bytes = ? AND status = 0", req.Hash, req.Size).
		Order("id DESC").First(&sess).Error; err == nil && sess.ExpiresAt != nil && sess.ExpiresAt.After(time.Now()) {
		var uploaded []int
		_ = json.Unmarshal([]byte(sess.UploadedChunks), &uploaded)
		ok(c, gin.H{"exists": false, "resumable": true, "session_id": sess.SessionID,
			"uploaded_chunks": uploaded, "chunk_size": sess.ChunkSize, "library_id": req.LibraryID})
		return
	}
	ok(c, gin.H{"exists": false, "resumable": false, "library_id": req.LibraryID})
}

type uploadInitReq struct {
	LibraryID  int64  `json:"library_id"`
	Hash       string `json:"hash"`
	Size       int64  `json:"size"`
	Filename   string `json:"filename"`
	ChunkSize  int    `json:"chunk_size"`
	DeviceID   int64  `json:"device_id"`
	LocalPath  string `json:"local_path"` // 手机端原始路径，仅记录展示
	TakenAt    string `json:"taken_at"`   // RFC3339，可空
	Network    string `json:"network"`    // wifi / mobile，决定默认分片大小
}

// uploadInit 初始化上传会话
func (s *Server) uploadInit(c *gin.Context) {
	var req uploadInitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if req.LibraryID == 0 && req.DeviceID > 0 {
		if libID, err := s.ensureDeviceLibrary(c, req.DeviceID); err == nil {
			req.LibraryID = libID
		}
	}
	if req.LibraryID == 0 || req.Size <= 0 {
		fail(c, http.StatusBadRequest, "library_id / size 必填")
		return
	}
	d, exist := s.drivers.Get(req.LibraryID)
	if !exist {
		fail(c, 500, "驱动未注册")
		return
	}
	if !d.Writable() {
		fail(c, http.StatusForbidden, "目标库为只读（挂载目录），不能上传")
		return
	}

	chunkSize := req.ChunkSize
	if chunkSize <= 0 {
		if strings.EqualFold(req.Network, "mobile") {
			chunkSize = s.cfg.Sync.ChunkSizeMobile
		} else {
			chunkSize = s.cfg.Sync.ChunkSizeWifi
		}
	}
	chunkTotal := int((req.Size + int64(chunkSize) - 1) / int64(chunkSize))

	sid := fmt.Sprintf("%d_%s", time.Now().UnixNano(), randSuffix())
	tmp := filepath.Join(s.cfg.Storage.TempRoot, sid)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		fail(c, 500, "创建暂存目录失败")
		return
	}
	ttl := s.cfg.Sync.SessionTTLHours
	if ttl <= 0 {
		ttl = 168
	}
	expires := time.Now().Add(time.Duration(ttl) * time.Hour)
	now := time.Now()
	sess := model.UploadSession{
		SessionID: sid, UserID: CurrentUserID(c), LibraryID: req.LibraryID,
		FileHash: req.Hash, SizeBytes: req.Size, ChunkSize: chunkSize,
		ChunkTotal: chunkTotal, UploadedChunks: "[]", TmpDir: tmp,
		Status: 0, ExpiresAt: &expires, CreatedAt: &now, UpdatedAt: &now,
	}
	if req.DeviceID > 0 {
		sess.DeviceID = &req.DeviceID
	}
	if err := s.db.Create(&sess).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, gin.H{"session_id": sid, "chunk_size": chunkSize, "chunk_total": chunkTotal,
		"uploaded_chunks": []int{}, "expires_at": expires})
}

// uploadChunk 上传单个分片：PUT /api/v1/upload/chunk?session_id=xx&index=0（body 为分片原始字节）
func (s *Server) uploadChunk(c *gin.Context) {
	sid := c.Query("session_id")
	idx, err := strconv.Atoi(c.Query("index"))
	if sid == "" || err != nil || idx < 0 {
		fail(c, http.StatusBadRequest, "参数错误: session_id / index")
		return
	}
	var sess model.UploadSession
	if err := s.db.Where("session_id = ?", sid).First(&sess).Error; err != nil {
		fail(c, 404, "会话不存在或已过期")
		return
	}
	if sess.Status != 0 || (sess.ExpiresAt != nil && sess.ExpiresAt.Before(time.Now())) {
		fail(c, 410, "会话已过期，请重新初始化")
		return
	}
	if idx >= sess.ChunkTotal {
		fail(c, http.StatusBadRequest, "分片序号越界")
		return
	}

	part := filepath.Join(sess.TmpDir, fmt.Sprintf("part_%06d", idx))
	f, err := os.Create(part)
	if err != nil {
		fail(c, 500, "写入分片失败")
		return
	}
	defer f.Close()
	written, err := io.Copy(f, c.Request.Body)
	if err != nil {
		fail(c, 500, "写入分片失败: "+err.Error())
		return
	}

	// 并发上传时，多个分片同时更新 uploaded_chunks 会有读-改-写竞争：
	// worker A 读 [0..19]，worker B 也读 [0..19]，A 写 [0..20]，B 写 [0..21] 覆盖 A → 丢分片。
	// 用 MySQL JSON_ARRAY_APPEND 原子追加，避免覆盖。已存在则不重复加（JSON_CONTAINS 判断）。
	existJSON := s.db.Model(&model.UploadSession{}).
		Where("id = ? AND JSON_CONTAINS(uploaded_chunks, CAST(? AS JSON))", sess.ID, idx).
		Update("updated_at", time.Now()).RowsAffected
	if existJSON == 0 {
		// 不存在 → 原子追加
		s.db.Exec(`UPDATE upload_sessions SET uploaded_chunks = JSON_ARRAY_APPEND(uploaded_chunks, '$', ?), updated_at = ? WHERE id = ?`,
			idx, time.Now(), sess.ID)
	}

	// 重新查一次返回最新列表
	var fresh model.UploadSession
	s.db.Select("uploaded_chunks").First(&fresh, sess.ID)
	var uploaded []int
	_ = json.Unmarshal([]byte(fresh.UploadedChunks), &uploaded)
	sort.Ints(uploaded)

	ok(c, gin.H{"index": idx, "bytes": written, "uploaded_chunks": uploaded,
		"total": sess.ChunkTotal})
}

type uploadCompleteReq struct {
	SessionID string `json:"session_id"`
	Filename  string `json:"filename"`
	DeviceID  int64  `json:"device_id"`
	LocalPath string `json:"local_path"`
	TakenAt   string `json:"taken_at"`
}

// uploadComplete 合并分片 → 校验哈希 → 写入托管存储 → 入库
func (s *Server) uploadComplete(c *gin.Context) {
	var req uploadCompleteReq
	if err := c.ShouldBindJSON(&req); err != nil || req.SessionID == "" {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	var sess model.UploadSession
	if err := s.db.Where("session_id = ?", req.SessionID).First(&sess).Error; err != nil {
		fail(c, 404, "会话不存在")
		return
	}
	var uploaded []int
	_ = json.Unmarshal([]byte(sess.UploadedChunks), &uploaded)
	if len(uploaded) < sess.ChunkTotal {
		fail(c, 409, fmt.Sprintf("分片不完整：%d/%d", len(uploaded), sess.ChunkTotal))
		return
	}
	// 合并前必须按分片序号升序排，否则文件内容错乱 → 视频损坏只能播 2 秒
	sort.Ints(uploaded)

	// 1. 合并
	merged := filepath.Join(sess.TmpDir, "merged.bin")
	out, err := os.Create(merged)
	if err != nil {
		fail(c, 500, "合并失败")
		return
	}
	h := sha256.New()
	for _, idx := range uploaded {
		part := filepath.Join(sess.TmpDir, fmt.Sprintf("part_%06d", idx))
		pf, err := os.Open(part)
		if err != nil {
			out.Close()
			fail(c, 500, "分片缺失: "+part)
			return
		}
		if _, err := io.Copy(io.MultiWriter(out, h), pf); err != nil {
			pf.Close()
			out.Close()
			fail(c, 500, "合并失败")
			return
		}
		pf.Close()
	}
	out.Close()

	sum := hex.EncodeToString(h.Sum(nil))

	// 注意（大坑）：手机端对 ≥20MB 的文件算的是「头 1MB + 尾 1MB + 文件大小」采样哈希
	// （app/utils/hash.js 的 fast 模式，和 storage.QuickHash 同一套算法），
	// 这里如果只拿全文件 SHA-256 去比，大文件（视频、高像素照片）必定误判为损坏 → 同步失败。
	// 因此：全量哈希、采样哈希二者任一匹配即通过。
	fullMax := int64(s.cfg.Sync.HashFullMaxBytes)
	if fullMax <= 0 {
		fullMax = 20 << 20
	}
	dbHash, dbAlgo, herr := storage.QuickHash(merged, fullMax)
	if herr != nil {
		dbHash, dbAlgo = sum, "sha256"
	}
	matched := strings.EqualFold(sum, sess.FileHash) || strings.EqualFold(dbHash, sess.FileHash)
	// 兜底再加一道：合并后的实际大小必须和会话登记的一致
	sizeOK := true
	if st, sterr := os.Stat(merged); sterr == nil {
		sizeOK = st.Size() == sess.SizeBytes
	}
	if sess.FileHash != "" && (!matched || !sizeOK) {
		_ = os.Remove(merged)
		fail(c, 400, "哈希校验失败，文件可能已损坏（期望 "+shortHash(sess.FileHash)+"… 实得 "+shortHash(dbHash)+"…，大小校验 "+strconv.FormatBool(sizeOK)+"）")
		return
	}

	// 内容魔数校验：扩展名声称是图片/视频，但文件头对不上 → 拒绝入库。
	// 典型场景：App 端读文件损坏（如 plus.android 桥接 bug 读出全零流），
	// 哈希和大小都「自洽」，只有魔数能识破。没有这道关，垃圾会进库且缩略图全部 500。
	cName := req.Filename
	if cName == "" {
		cName = "merged.bin"
	}
	if !checkContentMagic(merged, filepath.Ext(cName)) {
		_ = os.Remove(merged)
		_ = os.RemoveAll(sess.TmpDir)
		fail(c, 400, "文件内容校验失败：内容与文件格式不符（常见原因是 App 端读取文件损坏，请升级 App 后重新同步）")
		return
	}

	// 2. 秒传：合并后再次确认（避免并发重复上传）
	//    这里用 dbHash（与扫描同源），保证和后续扫描得到的 hash 一致，否则重扫会重复入库
	var existMedia model.MediaFile
	if err := s.db.Where("hash = ? AND deleted_at IS NULL", dbHash).First(&existMedia).Error; err == nil {
		_ = os.RemoveAll(sess.TmpDir)
		s.db.Model(&model.UploadSession{}).Where("id = ?", sess.ID).
			Updates(map[string]any{"status": 1, "updated_at": time.Now()})
		ok(c, gin.H{"media_id": existMedia.ID, "dedup": true})
		return
	}

	// 3. 写入托管存储
	d, exist := s.drivers.Get(sess.LibraryID)
	if !exist {
		fail(c, 500, "驱动未注册")
		return
	}
	src, err := os.Open(merged)
	if err != nil {
		fail(c, 500, "读取合并文件失败")
		return
	}
	defer src.Close()

	name := req.Filename
	if name == "" {
		name = filepath.Base(merged)
	}
	relPath := buildRelPath(name)

	res, err := d.UploadFile(c, src, storage.UploadOption{
		RelPath: relPath, Size: sess.SizeBytes, Mime: detectMime(merged),
		Mtime: time.Now(), Overwrite: false,
	})
	if err != nil {
		fail(c, 500, "落盘失败: "+err.Error())
		return
	}

	// 4. 解析元数据
	now := time.Now()
	mf := model.MediaFile{
		LibraryID:    sess.LibraryID,
		SourceType:   model.SourceManaged,
		RelativePath: res.RelPath,
		Filename:     filepath.Base(res.RelPath),
		Ext:          storage.ExtOf(res.RelPath),
		Mime:         detectMime(res.AbsPath),
		SizeBytes:    res.Size,
		Hash:         dbHash,
		HashAlgo:     dbAlgo,
		MediaType:    storage.MediaTypeOf(storage.ExtOf(res.RelPath)),
		IndexStatus:  model.IndexNormal,
		TakenAt:      &now,
		TakenAtSource: 2,
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	if sess.DeviceID != nil {
		mf.UploadedByDeviceID = sess.DeviceID
		mf.OriginalLocalPath = req.LocalPath
	}
	if meta, merr := media.Parse(res.AbsPath); merr == nil && meta != nil {
		if meta.TakenAt != nil {
			mf.TakenAt = meta.TakenAt
		}
		mf.TakenAtSource = meta.TakenSource
		mf.CameraMake, mf.CameraModel = meta.Make, meta.Model
		mf.GPSLat, mf.GPSLon = meta.Lat, meta.Lon
		mf.Orientation = meta.Orientation
		mf.ExifJSON = meta.ExifJSON
		mf.Width, mf.Height = meta.Width, meta.Height
		mf.DurationMs = meta.DurationMs
	}
	if req.TakenAt != "" {
		if t, terr := time.Parse(time.RFC3339, req.TakenAt); terr == nil {
			mf.TakenAt = &t
			mf.TakenAtSource = 1
		}
	}

	// 5. 幂等保护：按「库 + 相对路径」查是否已有同路径记录，避免撞 uk_media_lib_path 唯一键 → 500。
	//    注意必须 Unscoped（含软删除）：唯一键不含 deleted_at，软删行同样占用键位；
	//    只查存活行会漏判 → INSERT 撞 1062 → 500（v1.8.3 的坑）。
	var existMF model.MediaFile
	foundDup := false
	for _, rp := range []string{relPath, res.RelPath} {
		if rp == "" {
			continue
		}
		if err := s.db.Unscoped().
			Where("library_id = ? AND relative_path = ?", sess.LibraryID, rp).
			First(&existMF).Error; err == nil {
			foundDup = true
			break
		}
	}
	if foundDup {
		// 软删除的记录 = 用户此前在网页端删过该文件：尊重删除，丢弃本次上传
		//（文件在盘上必然是本次新写的或 renamed 孤儿，删掉不损失任何数据）
		if existMF.DeletedAt != nil {
			_ = os.Remove(res.AbsPath)
			_ = os.RemoveAll(sess.TmpDir)
			s.db.Model(&model.UploadSession{}).Where("id = ?", sess.ID).
				Updates(map[string]any{"status": 1, "relative_path": relPath, "updated_at": time.Now()})
			ok(c, gin.H{"media_id": existMF.ID, "dedup": true, "deleted": true, "path": relPath})
			return
		}
		if res.Renamed {
			// 写成了 `xxx (1).jpg` 孤儿，原文件还在磁盘，删掉孤儿、返回原行
			_ = os.Remove(res.AbsPath)
		} else {
			// 文件原本丢失、本次已覆盖写回 → 用新内容修正该行，恢复可预览
			s.db.Model(&model.MediaFile{}).Where("id = ?", existMF.ID).Updates(map[string]any{
				"hash":            dbHash,
				"hash_algo":       dbAlgo,
				"size_bytes":      res.Size,
				"mime":            detectMime(res.AbsPath),
				"width":           mf.Width,
				"height":          mf.Height,
				"duration_ms":     mf.DurationMs,
				"taken_at":        mf.TakenAt,
				"taken_at_source": mf.TakenAtSource,
				"updated_at":      time.Now(),
			})
		}
		_ = os.RemoveAll(sess.TmpDir)
		s.db.Model(&model.UploadSession{}).Where("id = ?", sess.ID).
			Updates(map[string]any{"status": 1, "relative_path": relPath, "updated_at": time.Now()})
		s.invalidate(cache.PfxMedia)
		ok(c, gin.H{"media_id": existMF.ID, "dedup": true, "restored": !res.Renamed, "path": relPath})
		return
	}

	if err := s.db.Create(&mf).Error; err != nil {
		if isDuplicateKey(err) {
			// 兜底：并发上传或软删行占键撞 uk_media_lib_path，按已存在处理而非 500
			var dup model.MediaFile
			if e := s.db.Unscoped().
				Where("library_id = ? AND relative_path = ?", sess.LibraryID, res.RelPath).
				Order("id DESC").First(&dup).Error; e == nil {
				if dup.DeletedAt != nil {
					_ = os.Remove(res.AbsPath)
					_ = os.RemoveAll(sess.TmpDir)
					s.db.Model(&model.UploadSession{}).Where("id = ?", sess.ID).
						Updates(map[string]any{"status": 1, "relative_path": relPath, "updated_at": time.Now()})
					ok(c, gin.H{"media_id": dup.ID, "dedup": true, "deleted": true, "path": relPath})
					return
				}
				// 存活行：文件丢失时本次已覆盖写回 → 修正该行恢复预览；renamed 孤儿删掉
				if res.Renamed {
					_ = os.Remove(res.AbsPath)
				} else {
					s.db.Model(&model.MediaFile{}).Where("id = ?", dup.ID).Updates(map[string]any{
						"hash": dbHash, "hash_algo": dbAlgo, "size_bytes": res.Size,
						"mime": detectMime(res.AbsPath), "updated_at": time.Now(),
					})
				}
				_ = os.RemoveAll(sess.TmpDir)
				s.db.Model(&model.UploadSession{}).Where("id = ?", sess.ID).
					Updates(map[string]any{"status": 1, "relative_path": relPath, "updated_at": time.Now()})
				s.invalidate(cache.PfxMedia)
				ok(c, gin.H{"media_id": dup.ID, "dedup": true, "restored": !res.Renamed, "path": relPath})
				return
			}
		}
		fail(c, 500, "入库失败: "+err.Error())
		return
	}

	// 5. 预生成缩略图（后台队列，不阻塞响应），确保 web 端立即能预览
	if tp, ok := storage.ThumbProviderInstance(); ok {
		tp.Prewarm(d, mf)
	}
	// 刷新库媒体计数
	s.db.Exec("UPDATE libraries SET media_count = (SELECT COUNT(*) FROM media_files WHERE library_id = ? AND deleted_at IS NULL) WHERE id = ?", sess.LibraryID, sess.LibraryID)
	s.invalidate(cache.PfxMedia)

	// 6. 清理
	_ = os.RemoveAll(sess.TmpDir)
	s.db.Model(&model.UploadSession{}).Where("id = ?", sess.ID).
		Updates(map[string]any{"status": 1, "relative_path": res.RelPath, "updated_at": time.Now()})

	ok(c, gin.H{"media_id": mf.ID, "dedup": false, "renamed": res.Renamed, "path": res.RelPath})
}

// buildRelPath 托管存储落盘路径：YYYY/MM/filename
func buildRelPath(filename string) string {
	now := time.Now()
	filename = filepath.Base(filename)
	return fmt.Sprintf("%04d/%02d/%s", now.Year(), int(now.Month()), filename)
}

func detectMime(path string) string {
	if mt, err := mimetype.DetectFile(path); err == nil && mt != nil {
		return mt.String()
	}
	return "application/octet-stream"
}

// checkContentMagic 校验文件内容魔数与扩展名是否相符。
// 背景：App 端 plus.android 桥接读大文件曾产出「全零流」，哈希与大小都自洽，
// 只有文件头能识破（vips 报 unsupported image format、缩略图全部 500）。
// 未知扩展名不校验；打不开文件也交给后续流程报错。
func checkContentMagic(path, ext string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	head := make([]byte, 16)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF
	case ".png":
		return bytes.HasPrefix(head, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	case ".gif":
		return bytes.HasPrefix(head, []byte("GIF8"))
	case ".webp":
		return len(head) >= 12 && bytes.HasPrefix(head, []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP"))
	case ".heic", ".heif", ".hif", ".mp4", ".mov", ".m4v":
		return len(head) >= 12 && bytes.Equal(head[4:8], []byte("ftyp"))
	case ".bmp":
		return bytes.HasPrefix(head, []byte("BM"))
	default:
		return true
	}
}

// shortHash 取哈希前 8 位用于日志/报错展示（不足则不截断）
func shortHash(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// isDuplicateKey 判断是否为唯一键冲突（MySQL 1062 / PostgreSQL 23505）
func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") ||
		strings.Contains(msg, "1062") ||
		strings.Contains(msg, "23505") ||
		strings.Contains(msg, "UNIQUE")
}

// randSuffix 生成会话随机后缀
func randSuffix() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// ensureDeviceLibrary 查找或创建以设备名命名的托管库
// 每台手机一个独立库，库名 = 设备名（device_name），库类型 = 托管
func (s *Server) ensureDeviceLibrary(c *gin.Context, deviceID int64) (int64, error) {
	var dev model.SyncDevice
	if err := s.db.First(&dev, deviceID).Error; err != nil {
		return 0, err
	}
	libName := dev.DeviceName
	if libName == "" {
		libName = "Device-" + strconv.FormatInt(deviceID, 10)
	}
	// 先找已存在的同名托管库
	var lib model.Library
	if err := s.db.Where("name = ? AND type = ? AND deleted_at IS NULL", libName, model.LibraryTypeManaged).
		First(&lib).Error; err == nil {
		// 找到，确保驱动已注册
		if _, ok := s.drivers.Get(lib.ID); !ok {
			_ = s.registerLibraryDriver(lib)
		}
		return lib.ID, nil
	}
	// 不存在则创建
	root := filepath.Join(s.cfg.Storage.ManagedRoot, slug(libName))
	now := time.Now()
	lib = model.Library{
		Name: libName, Type: model.LibraryTypeManaged,
		OwnerID:     firstPositive(dev.UserID, CurrentUserID(c)),
		StorageRoot: root, Status: 1,
		CreatedAt: &now, UpdatedAt: &now,
	}
	if err := s.db.Create(&lib).Error; err != nil {
		return 0, err
	}
	if err := s.registerLibraryDriver(lib); err != nil {
		return 0, err
	}
	s.invalidate(cache.PfxLibrary)
	return lib.ID, nil
}
