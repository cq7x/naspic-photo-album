// Package thumb 缩略图服务：懒加载生成 + 独立缓存目录 + 并发队列。
// 设计目标：浏览才生成（不批量预生成）、缓存与原图彻底分离、ARM 低配限制并发。
package thumb

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naspic/naspic/internal/config"
	"github.com/naspic/naspic/internal/model"
	"github.com/naspic/naspic/internal/storage"
)

// ffmpegBin 视频封面（首帧）抽取依赖 ffmpeg，缺失时视频缩略图降级为占位图
var ffmpegBin string

// failCooldown 记录「最近生成失败」的媒体，短时间內不再重复解码。
//
// 为什么不用 thumb_status=2 那种永久拉黑：失败原因很多是可恢复的——
// 目录忘了挂进容器、移动硬盘临时拔了、ffmpeg 是后装的……
// 一旦永久标记，环境修好了照片也永远不会有缩略图。
// 这里改成「失败后冷却 5 分钟」，既不会每次浏览都重试拖慢列表，
// 又能自愈；服务重启也会自然清空。
var failCooldown sync.Map // mediaID(int64) -> time.Time

const failCooldownDur = 5 * time.Minute

func init() {
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		ffmpegBin = p
	}
}

// FFMPEGAvailable 视频封面能力是否可用（供启动日志提示）
func FFMPEGAvailable() bool { return ffmpegBin != "" }

// Generator 缩略图后端（libvips / 纯 Go 降级）
type Generator interface {
	Name() string
	Generate(src, dst string, size int, quality int, format string) error
}

// 由 generate_vips.go / generate_go.go 按 build tag 注入
var defaultGenerator Generator

// SizeKey 尺寸规格 → 像素
var sizes = map[storage.ThumbSize]int{
	storage.ThumbSM: 256,
	storage.ThumbMD: 512,
	storage.ThumbLG: 1080,
	storage.ThumbSQ: 300,
}

// Service 缩略图服务
type Service struct {
	db   *gorm.DB
	cfg  *config.Config
	gen  Generator
	mu   sync.Mutex
	busy map[string]chan struct{} // 正在生成的 key → 完成信号，防重复生成 + 可等待
	genQ chan genTask        // 后台补生成队列
}

type genTask struct {
	mediaID  int64
	srcPath  string
	cacheAbs string
	size     int
	quality  int
	format   string
	isVideo  bool
}

// New 创建缩略图服务并启动工作池
func New(db *gorm.DB, cfg *config.Config) *Service {
	if cfg.Thumb.Workers <= 0 {
		cfg.Thumb.Workers = 1
	}
	if cfg.Thumb.Quality <= 0 {
		cfg.Thumb.Quality = 82
	}
	if cfg.Thumb.Format == "" {
		cfg.Thumb.Format = "webp"
	}
	// 自定义尺寸：thumb.sizes = "256,512,1080"
	if s := cfg.Thumb.Sizes; s != "" {
		parts := strings.Split(s, ",")
		if len(parts) >= 3 {
			for i, k := range []storage.ThumbSize{storage.ThumbSM, storage.ThumbMD, storage.ThumbLG} {
				if v, err := strconv.Atoi(strings.TrimSpace(parts[i])); err == nil && v > 0 {
					sizes[k] = v
				}
			}
		}
	}

	s := &Service{
		db:   db,
		cfg:  cfg,
		gen:  defaultGenerator,
		busy: make(map[string]chan struct{}),
		genQ: make(chan genTask, cfg.Thumb.QueueSize),
	}
	if s.gen == nil {
		s.gen = &nilGenerator{}
	}
	// 缓存目录（必须与原图分离，且可独立清理/挂载到 SSD）
	_ = os.MkdirAll(filepath.Join(cfg.Storage.CacheRoot, "thumbs"), 0o755)

	for i := 0; i < cfg.Thumb.Workers; i++ {
		go s.worker()
	}
	log.Printf("[thumb] 缩略图服务就绪 backend=%s workers=%d", s.gen.Name(), cfg.Thumb.Workers)
	if ffmpegBin == "" {
		log.Printf("[thumb] 未检测到 ffmpeg：视频无法抽帧，列表里将显示占位封面")
	}
	return s
}

// Get 取缩略图：命中缓存直接返回，未命中现场生成（懒加载）
func (s *Service) Get(ctx context.Context, drv storage.StorageDriver, key string, size storage.ThumbSize) ([]byte, string, error) {
	px, ok := sizes[size]
	if !ok {
		px = sizes[storage.ThumbMD]
	}

	var m model.MediaFile
	if err := s.db.Where("library_id = ? AND relative_path = ? AND deleted_at IS NULL", drv.LibraryID(), key).
		First(&m).Error; err != nil {
		return nil, "", storage.ErrNotFound
	}
	if m.IndexStatus == model.IndexMissing {
		return nil, "", errors.New("源文件已缺失")
	}
	// 冷却期内不重试：坏文件/缺目录不该被每次浏览反复解码
	if v, ok := failCooldown.Load(m.ID); ok {
		if t, ok := v.(time.Time); ok && time.Since(t) < failCooldownDur {
			return nil, "", errors.New("刚刚生成失败，稍后自动重试")
		}
		failCooldown.Delete(m.ID)
	}
	// thumb_status=2 是老版本留下的「永久失败」标记。失败原因多半是可恢复的，
	// 这里一律放行重来，频率控制交给上面的 failCooldown。
	if m.ThumbStatus == 2 {
		s.db.Model(&model.MediaFile{}).Where("id = ?", m.ID).Update("thumb_status", 0)
		m.ThumbStatus = 0
	}

	format := s.cfg.Thumb.Format
	if format == "webp" && s.gen.Name() == "go" {
		format = "jpeg" // 纯 Go 后端不支持 WebP 编码，降级为 JPEG
	}
	// 视频没有「缩略图」这回事：抽一帧当封面，ffmpeg 直接输出 JPEG 最稳
	isVideo := m.MediaType == model.MediaVideo
	if isVideo {
		format = "jpeg"
		if ffmpegBin == "" {
			return nil, "", errors.New("视频封面需要 ffmpeg，镜像内未安装")
		}
	}
	cacheAbs := s.cachePath(drv.LibraryID(), m.Hash, string(size), px, format)

	// ① 命中磁盘缓存
	//    注意必须校验 len(b) > 0：并发下可能读到「刚被 os.Create 还没写入」的空文件，
	//    直接返回会让前端拿到 0 字节的破图。
	if b, err := os.ReadFile(cacheAbs); err == nil && len(b) > 0 {
		return b, contentType(format), nil
	}

	// ② 生成（单飞：同一文件并发请求只生成一次）
	srcAbs, err := storage.SafeJoin(drv.Root(), m.RelativePath)
	if err != nil {
		return nil, "", err
	}
	done := s.claim(cacheAbs)
	if done == nil {
		// 已有协程在生成：等它写完后重读，别把 0 字节的空图丢给前端
		s.waitPending(cacheAbs, 15*time.Second)
		if b, err := os.ReadFile(cacheAbs); err == nil && len(b) > 0 {
			return b, contentType(format), nil
		}
		return nil, "", storage.ErrThumbPending
	}
	defer s.release(cacheAbs, done)

	if err := os.MkdirAll(filepath.Dir(cacheAbs), 0o755); err != nil {
		return nil, "", err
	}
	if err := s.generate(srcAbs, cacheAbs, px, s.cfg.Thumb.Quality, format, isVideo); err != nil {
		// 不再写 thumb_status=2：那等于永久拉黑，环境恢复了也救不回来。
		// 只登记冷却，5 分钟后（或重启后）自动再试一次。
		failCooldown.Store(m.ID, time.Now())
		log.Printf("[thumb] 生成失败 id=%d video=%v err=%v（%v 后自动重试）",
			m.ID, isVideo, err, failCooldownDur)
		return nil, "", errors.New("缩略图生成失败")
	}
	failCooldown.Delete(m.ID)

	st, statErr := os.Stat(cacheAbs)
	rel, _ := filepath.Rel(s.cfg.Storage.CacheRoot, cacheAbs)
	now := time.Now()
	if st == nil || statErr != nil {
		log.Printf("[thumb] 生成后无法读取缓存文件 id=%d err=%v", m.ID, statErr)
		return nil, "", errors.New("缩略图生成结果不可用")
	}
	// 清理过缓存后会重新生成，此时 thumbnails 里往往已有同 (media_id, size_key) 的记录。
	// 用 upsert 而不是 Save，否则每次都撞唯一约束、刷一屏 constraint failed 日志。
	s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "media_id"}, {Name: "size_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"rel_path", "bytes", "source_hash", "status", "updated_at",
		}),
	}).Create(&model.Thumbnail{
		MediaID: m.ID, SizeKey: string(size), RelPath: filepath.ToSlash(rel),
		Bytes: int(st.Size()), SourceHash: m.Hash, Status: 1,
		CreatedAt: &now, UpdatedAt: &now,
	})
	s.db.Model(&model.MediaFile{}).Where("id = ?", m.ID).Update("thumb_status", 1)

	b, err := os.ReadFile(cacheAbs)
	if err != nil || len(b) == 0 {
		if err == nil {
			err = errors.New("缩略图生成结果为空")
		}
		return nil, "", err
	}
	return b, contentType(format), nil
}

// EnqueueAll 后台补生成：把库中还没生成过缩略图的媒体排队（仍走限流工作池）。
// 注意必须带上 srcPath / cacheAbs，否则 worker 会因为路径为空直接跳过（历史 bug）。
func (s *Service) EnqueueAll(drv storage.StorageDriver) int {
	var list []model.MediaFile
	s.db.Where("library_id = ? AND thumb_status = 0 AND deleted_at IS NULL", drv.LibraryID()).
		Limit(5000).Find(&list)
	n := 0
	for _, m := range list {
		srcAbs, err := storage.SafeJoin(drv.Root(), m.RelativePath)
		if err != nil {
			continue
		}
		px := sizes[storage.ThumbMD]
		format := s.cfg.Thumb.Format
		if m.MediaType == model.MediaVideo {
			format = "jpeg"
		}
		select {
		case s.genQ <- genTask{
			mediaID:  m.ID,
			srcPath:  srcAbs,
			cacheAbs: s.cachePath(drv.LibraryID(), m.Hash, string(storage.ThumbMD), px, format),
			size:     px,
			quality:  s.cfg.Thumb.Quality,
			format:   format,
			isVideo:  m.MediaType == model.MediaVideo,
		}:
			n++
		default:
			return n
		}
	}
	return n
}

// Prewarm 上传/扫描完成后预先生成一张缩略图（后台队列，不阻塞请求）
func (s *Service) Prewarm(drv storage.StorageDriver, m model.MediaFile) {
	srcAbs, err := storage.SafeJoin(drv.Root(), m.RelativePath)
	if err != nil {
		return
	}
	px := sizes[storage.ThumbSM]
	format := s.cfg.Thumb.Format
	if m.MediaType == model.MediaVideo {
		format = "jpeg"
		if ffmpegBin == "" {
			return
		}
	}
	select {
	case s.genQ <- genTask{
		mediaID:  m.ID,
		srcPath:  srcAbs,
		cacheAbs: s.cachePath(drv.LibraryID(), m.Hash, string(storage.ThumbSM), px, format),
		size:     px,
		quality:  s.cfg.Thumb.Quality,
		format:   format,
		isVideo:  m.MediaType == model.MediaVideo,
	}:
	default: // 队列满了就算了，前端浏览时会懒加载补上
	}
}

// ---------- 内部 ----------

func (s *Service) worker() {
	for t := range s.genQ {
		if t.srcPath == "" || t.cacheAbs == "" {
			continue
		}
		if err := s.generate(t.srcPath, t.cacheAbs, t.size, s.cfg.Thumb.Quality, t.format, t.isVideo); err != nil {
			failCooldown.Store(t.mediaID, time.Now())
			log.Printf("[thumb] 后台生成失败 id=%d err=%v", t.mediaID, err)
			continue
		}
		failCooldown.Delete(t.mediaID)
		now := time.Now()
		rel, _ := filepath.Rel(s.cfg.Storage.CacheRoot, t.cacheAbs)
		s.db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "media_id"}, {Name: "size_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"rel_path", "status", "updated_at",
			}),
		}).Create(&model.Thumbnail{
			MediaID: t.mediaID, SizeKey: string(storage.ThumbMD),
			RelPath: filepath.ToSlash(rel), Status: 1, CreatedAt: &now, UpdatedAt: &now,
		})
		s.db.Model(&model.MediaFile{}).Where("id = ?", t.mediaID).Update("thumb_status", 1)
	}
}

// generate 统一生成入口：图片走 libvips / 纯 Go 后端，视频抽帧当封面。
func (s *Service) generate(srcAbs, cacheAbs string, px, quality int, format string, isVideo bool) error {
	if err := os.MkdirAll(filepath.Dir(cacheAbs), 0o755); err != nil {
		return err
	}
	if isVideo {
		return s.genVideoPoster(srcAbs, cacheAbs, px)
	}
	return s.gen.Generate(srcAbs, cacheAbs, px, quality, format)
}

// genVideoPoster 用 ffmpeg 抽视频封面帧（先试第 1 秒，失败再退回第 0 秒）。
// 输出统一为 JPEG：不依赖 libwebp，且浏览器 <video poster> 兼容性最好。
func (s *Service) genVideoPoster(src, dst string, px int) error {
	if ffmpegBin == "" {
		return errors.New("ffmpeg 不可用")
	}
	tmp := dst + ".tmp.jpg"
	_ = os.Remove(tmp)
	var lastErr error
	for _, ss := range []string{"1", "0"} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, ffmpegBin,
			"-hide_banner", "-loglevel", "error", "-y",
			"-ss", ss, "-i", src, "-frames:v", "1",
			"-vf", fmt.Sprintf("scale=%d:-2", px), "-q:v", "4", tmp)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("%v %s", err, strings.TrimSpace(string(out)))
			continue
		}
		st, serr := os.Stat(tmp)
		if serr != nil || st == nil || st.Size() == 0 {
			lastErr = errors.New("ffmpeg 未产出封面帧")
			continue
		}
		if rerr := os.Rename(tmp, dst); rerr != nil {
			return rerr
		}
		return nil
	}
	_ = os.Remove(tmp)
	if lastErr == nil {
		lastErr = errors.New("抽帧失败")
	}
	return lastErr
}

// claim 单飞控制：返回 nil 表示已有协程在生成（调用方应先 waitPending 再重试读取）
func (s *Service) claim(key string) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.busy[key]; ok {
		return nil
	}
	ch := make(chan struct{})
	s.busy[key] = ch
	return ch
}

// waitPending 等待「另一个协程」把同一张缩略图生成完（最多 timeout）。
// 没有等待方时直接返回。
func (s *Service) waitPending(key string, timeout time.Duration) {
	s.mu.Lock()
	ch, ok := s.busy[key]
	s.mu.Unlock()
	if !ok {
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
	case <-timer.C:
	}
}

func (s *Service) release(key string, ch chan struct{}) {
	s.mu.Lock()
	delete(s.busy, key)
	s.mu.Unlock()
	close(ch) // 唤醒所有等待方
}

// cachePath 缓存路径：<cache>/thumbs/<lib>/<hash[0:2]>/<hash>/<size>_<px>.<ext>
func (s *Service) cachePath(libraryID int64, hash string, sizeKey string, px int, format string) string {
	if len(hash) < 4 {
		hash = fmt.Sprintf("%064d", libraryID)
	}
	return filepath.Join(s.cfg.Storage.CacheRoot, "thumbs",
		strconv.FormatInt(libraryID, 10), hash[:2], hash,
		fmt.Sprintf("%s_%d.%s", sizeKey, px, format))
}

func contentType(format string) string {
	if format == "webp" {
		return "image/webp"
	}
	return "image/jpeg"
}

// nilGenerator 兜底：任何生成都失败，保证服务不崩
type nilGenerator struct{}

func (g *nilGenerator) Name() string { return "none" }
func (g *nilGenerator) Generate(src, dst string, size, quality int, format string) error {
	return errors.New("未启用缩略图后端，请使用 -tags vips 编译或检查配置")
}
