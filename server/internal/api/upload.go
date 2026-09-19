package api

import (
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

	"github.com/naspic/naspic/internal/media"
	"github.com/naspic/naspic/internal/model"
	"github.com/naspic/naspic/internal/storage"
)

// 上传三步：check（秒传探测）→ init（建会话）→ chunk（并发分片）→ complete（合并校验入库）

type uploadCheckReq struct {
	LibraryID int64  `json:"library_id"`
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
	var m model.MediaFile
	err := s.db.Where("hash = ? AND deleted_at IS NULL", req.Hash).
		Order("id DESC").First(&m).Error
	if err == nil {
		ok(c, gin.H{"exists": true, "media_id": m.ID})
		return
	}

	// 查进行中的会话，支持断点续传
	var sess model.UploadSession
	if err := s.db.Where("file_hash = ? AND size_bytes = ? AND status = 0", req.Hash, req.Size).
		Order("id DESC").First(&sess).Error; err == nil && sess.ExpiresAt != nil && sess.ExpiresAt.After(time.Now()) {
		var uploaded []int
		_ = json.Unmarshal([]byte(sess.UploadedChunks), &uploaded)
		ok(c, gin.H{"exists": false, "resumable": true, "session_id": sess.SessionID,
			"uploaded_chunks": uploaded, "chunk_size": sess.ChunkSize})
		return
	}
	ok(c, gin.H{"exists": false, "resumable": false})
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

	var uploaded []int
	_ = json.Unmarshal([]byte(sess.UploadedChunks), &uploaded)
	exist := false
	for _, v := range uploaded {
		if v == idx {
			exist = true
			break
		}
	}
	if !exist {
		uploaded = append(uploaded, idx)
		sort.Ints(uploaded)
	}
	b, _ := json.Marshal(uploaded)
	s.db.Model(&model.UploadSession{}).Where("id = ?", sess.ID).
		Updates(map[string]any{"uploaded_chunks": string(b), "updated_at": time.Now()})

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
	if sess.FileHash != "" && !strings.EqualFold(sum, sess.FileHash) {
		os.Remove(merged)
		fail(c, 400, "哈希校验失败，文件可能已损坏")
		return
	}

	// 2. 秒传：合并后再次确认（避免并发重复上传）
	var existMedia model.MediaFile
	if err := s.db.Where("hash = ? AND deleted_at IS NULL", sum).First(&existMedia).Error; err == nil {
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

	// 4. 解析元数据并入库
	now := time.Now()
	mf := model.MediaFile{
		LibraryID:    sess.LibraryID,
		SourceType:   model.SourceManaged,
		RelativePath: res.RelPath,
		Filename:     filepath.Base(res.RelPath),
		Ext:          storage.ExtOf(res.RelPath),
		Mime:         detectMime(res.AbsPath),
		SizeBytes:    res.Size,
		Hash:         res.Hash,
		HashAlgo:     "sha256",
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
	if err := s.db.Create(&mf).Error; err != nil {
		fail(c, 500, "入库失败: "+err.Error())
		return
	}

	// 5. 清理
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

// randSuffix 生成会话随机后缀
func randSuffix() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}
