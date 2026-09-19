// Package scanner 扫描调度：有界队列 + 工作池 + 令牌桶限流 + CPU 守护。
// 目标：在树莓派等低配 ARM 设备上，扫描几十万张照片也不把 CPU/IO 打满。
package scanner

import (
	"context"
	"errors"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naspic/naspic/internal/cache"
	"github.com/naspic/naspic/internal/config"
	"github.com/naspic/naspic/internal/media"
	"github.com/naspic/naspic/internal/model"
	"github.com/naspic/naspic/internal/storage"
)

// ErrJobRunning 该库已有扫描任务在跑
var ErrJobRunning = errors.New("scanner: 该相册库已有扫描任务在运行")

// JobRequest 扫描请求
type JobRequest struct {
	LibraryID  int64
	Full       bool  // 全量重建索引
	Force      bool  // 忽略扫描缓存，强制重算哈希与元数据（缓存疑似不准时用）
	TriggerSrc int8  // 1=手动 2=定时 3=inotify
}

// Manager 扫描管理器
type Manager struct {
	db      *gorm.DB
	cfg     *config.Config
	drivers *storage.Registry

	mu      sync.Mutex
	running map[int64]struct{} // 正在扫描的库
	queue   chan JobRequest
	limiter *rateLimiter
	wg      sync.WaitGroup
	quit    chan struct{}
}

// NewManager 创建扫描管理器并启动工作池
func NewManager(db *gorm.DB, cfg *config.Config, drivers *storage.Registry) *Manager {
	if cfg.Scan.QueueSize <= 0 {
		cfg.Scan.QueueSize = 512
	}
	if cfg.Scan.Workers <= 0 {
		cfg.Scan.Workers = 1
	}
	m := &Manager{
		db:      db,
		cfg:     cfg,
		drivers: drivers,
		running: make(map[int64]struct{}),
		queue:   make(chan JobRequest, cfg.Scan.QueueSize),
		limiter: newRateLimiter(cfg.Scan.RatePerSec),
		quit:    make(chan struct{}),
	}
	for i := 0; i < cfg.Scan.Workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	return m
}

// Submit 提交扫描任务（队列满时返回错误，避免无限堆积）
func (m *Manager) Submit(req JobRequest) error {
	m.mu.Lock()
	if _, ok := m.running[req.LibraryID]; ok {
		m.mu.Unlock()
		return ErrJobRunning
	}
	m.running[req.LibraryID] = struct{}{}
	m.mu.Unlock()

	select {
	case m.queue <- req:
		return nil
	default:
		m.mu.Lock()
		delete(m.running, req.LibraryID)
		m.mu.Unlock()
		return errors.New("scanner: 扫描队列已满，请稍后再试")
	}
}

// Stop 停止工作池
func (m *Manager) Stop() {
	close(m.quit)
	m.wg.Wait()
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.quit:
			return
		case req := <-m.queue:
			m.runJob(req)
			m.mu.Lock()
			delete(m.running, req.LibraryID)
			m.mu.Unlock()
		}
	}
}

// runJob 执行一次扫描：建 job 记录 → 消费事件 → 批量 upsert → 更新统计
func (m *Manager) runJob(req JobRequest) {
	drv, ok := m.drivers.Get(req.LibraryID)
	if !ok {
		log.Printf("[scanner] 库 %d 未注册驱动，跳过", req.LibraryID)
		return
	}

	job := model.ScanJob{
		LibraryID:  req.LibraryID,
		Type:       boolToType(req.Full),
		TriggerSrc: req.TriggerSrc,
		Status:     2,
		StartedAt:  timePtr(time.Now()),
		CreatedAt:  timePtr(time.Now()),
	}
	if err := m.db.Create(&job).Error; err != nil {
		log.Printf("[scanner] 创建扫描任务失败: %v", err)
		return
	}

	var lib model.Library
	_ = m.db.First(&lib, req.LibraryID).Error

	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opt := storage.ScanOption{Full: req.Full, Recursive: true}
	if !req.Full {
		// 增量：以该库最后一次扫描时间为起点，留 2 分钟重叠避免边界漏检
		var last model.ScanJob
		if err := m.db.Where("library_id = ? AND status = 3 AND id <> ?", req.LibraryID, job.ID).
			Order("id DESC").First(&last).Error; err == nil && last.StartedAt != nil {
			opt.Since = last.StartedAt.Add(-2 * time.Minute)
		}
	}

	ch, err := drv.Scan(ctx, opt)
	if err != nil {
		m.finishJob(&job, err.Error())
		return
	}

	batch := m.cfg.Scan.BatchSize
	if batch <= 0 {
		batch = 200
	}
	buf := make([]model.MediaFile, 0, batch)
	missing := 0
	// 本次扫描实际见到的路径。全量扫描结束时用它反查"磁盘上已消失但索引还在"的条目。
	seen := make(map[string]struct{}, 1024)

	// ---------- 扫描缓存 ----------
	// 文件没变（大小 + mtime 都没动）就没必要重算全文件哈希，这是扫描最大的一项开销。
	// force=false 时把该库的指纹缓存一次性载入内存，命中且索引已有记录就直接跳过。
	// 缓存只影响速度，清掉不会丢数据，最多是下一轮慢一点。
	cache := make(map[string]model.ScanCache)
	if !req.Force {
		var rows []model.ScanCache
		if err := m.db.Where("library_id = ?", req.LibraryID).
			Select("rel_path", "size_bytes", "mod_unix", "hash", "width", "height", "taken_at", "media_type", "hit_count").
			Find(&rows).Error; err == nil {
			for _, r := range rows {
				cache[r.RelPath] = r
			}
		}
	}
	cacheBuf := make([]model.ScanCache, 0, batch)
	cacheSeen := make(map[string]struct{}, 1024)
	hits := make([]string, 0, 256) // 本次命中的路径，收尾统一累加 hit_count
	flushCache := func() {
		if len(cacheBuf) == 0 {
			return
		}
		err := m.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "library_id"}, {Name: "rel_path"}},
			DoUpdates: clause.AssignmentColumns([]string{"size_bytes", "mod_unix", "hash", "width", "height", "taken_at", "media_type", "updated_at"}),
		}).Create(&cacheBuf).Error
		if err != nil {
			log.Printf("[scanner] 库 %d 扫描缓存写入失败: %v", req.LibraryID, err)
		}
		cacheBuf = cacheBuf[:0]
	}
	// mediaExists 索引里是否已有该路径（缓存命中仍需确认，否则会漏建记录）
	mediaExists := func(rel string) bool {
		var n int64
		m.db.Model(&model.MediaFile{}).
			Where("library_id = ? AND relative_path = ? AND deleted_at IS NULL", req.LibraryID, rel).
			Count(&n)
		return n > 0
	}

	flush := func() {
		if len(buf) == 0 {
			return
		}
		m.limiter.wait()
		m.waitCPU()
		// 文件确实存在于磁盘 → 先撤销软删标记。
		// 否则出现"删错了就永远扫不回来"：upsert 的 DoUpdates 不含 deleted_at，
		// 已软删的行会被更新字段但依然处于删除态，从此从相册里永久消失。
		for i := 0; i < len(buf); i += 500 {
			end := i + 500
			if end > len(buf) {
				end = len(buf)
			}
			keys := make([]string, 0, end-i)
			for _, it := range buf[i:end] {
				keys = append(keys, it.RelativePath)
			}
			m.db.Model(&model.MediaFile{}).
				Where("library_id = ? AND relative_path IN ? AND deleted_at IS NOT NULL", req.LibraryID, keys).
				Update("deleted_at", nil)
		}
		// 冲突时更新核心字段，保证重复扫描幂等
		err := m.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "library_id"}, {Name: "relative_path"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"size_bytes", "hash", "index_status", "updated_at", "ext", "media_type",
				"width", "height", "taken_at", "taken_at_source",
			}),
		}).Create(&buf).Error
		if err != nil {
			log.Printf("[scanner] 批量入库失败: %v", err)
			m.logf(job.ID, 3, "", err.Error())
			job.Failed += len(buf)
		}
		buf = buf[:0]
	}

	for ev := range ch {
		switch ev.Type {
		case storage.EventCreated, storage.EventUpdated:
			seen[ev.Entry.Key] = struct{}{}
			mt := ev.Entry.MediaType
			if mt == 0 {
				mt = storage.MediaTypeOf(ev.Entry.Ext)
			}
			modUnix := ev.Entry.ModTime.Unix()

			// 命中缓存：文件指纹没变 + 索引里已有记录 → 直接跳过（省掉哈希与解析）
			if c, ok := cache[ev.Entry.Key]; ok && c.SizeBytes == ev.Entry.Size && c.ModUnix == modUnix {
				if mediaExists(ev.Entry.Key) {
					cacheSeen[ev.Entry.Key] = struct{}{}
					hits = append(hits, ev.Entry.Key)
					job.Scanned++
					job.Skipped++
					continue
				}
			}

			taken := ev.Entry.ModTime
			takenSrc := int8(2)
			var w, h, dur int
			// 图片补采 EXIF 拍摄时间与像素尺寸；视频补采容器里的 creation_time 与时长。
			// 视频之前压根没解析，一直用文件 mtime——导出/复制过的文件 mtime 全乱，
			// 排序自然不对。media.Parse 对视频走 ffprobe 只读容器头，不解码画面。
			if (mt == 1 || mt == 2) && ev.Entry.AbsPath != "" { // 1=图片 2=视频
				if meta, merr := media.Parse(ev.Entry.AbsPath); merr == nil && meta != nil {
					if meta.TakenAt != nil {
						taken = *meta.TakenAt
						takenSrc = meta.TakenSource
					}
					w, h = meta.Width, meta.Height
					dur = meta.DurationMs
				}
			}
			now := time.Now()
			buf = append(buf, model.MediaFile{
				LibraryID:    req.LibraryID,
				SourceType:   sourceTypeOf(drv),
				RelativePath: ev.Entry.Key,
				Filename:     base(ev.Entry.Key),
				Ext:          ev.Entry.Ext,
				SizeBytes:    ev.Entry.Size,
				Hash:         ev.Hash,
				HashAlgo:     "sha256",
				MediaType:    mt,
				Width:        w,
				Height:       h,
				DurationMs:   dur,
				IndexStatus:  model.IndexNormal,
				TakenAt:      &taken,
				TakenAtSource: takenSrc,
				CreatedAt:     &now,
				UpdatedAt:     &now,
			})
			seen[ev.Entry.Key] = struct{}{}
			cacheSeen[ev.Entry.Key] = struct{}{}
			cacheBuf = append(cacheBuf, model.ScanCache{
				LibraryID: req.LibraryID,
				RelPath:   ev.Entry.Key,
				SizeBytes: ev.Entry.Size,
				ModUnix:   modUnix,
				Hash:      ev.Hash,
				Width:     w,
				Height:    h,
				TakenAt:   &taken,
				MediaType: mt,
				CreatedAt: &now,
				UpdatedAt: &now,
			})
			job.Added++
			job.Scanned++
			if len(buf) >= batch {
				flush()
			}
			if len(cacheBuf) >= batch {
				flushCache()
			}
		case storage.EventMissing, storage.EventDeleted:
			// 源文件消失：只读挂载模式只标记索引，绝不删宿主机任何东西
			m.db.Model(&model.MediaFile{}).
				Where("library_id = ? AND relative_path = ?", req.LibraryID, ev.Entry.Key).
				Update("index_status", model.IndexMissing)
			m.db.Where("library_id = ? AND rel_path = ?", req.LibraryID, ev.Entry.Key).
				Delete(&model.ScanCache{})
			delete(cache, ev.Entry.Key)
			missing++
			job.Missing++
		case storage.EventError:
			job.Failed++
			m.logf(job.ID, 3, ev.Entry.Key, ev.Msg)
		default:
			job.Scanned++
			// 驱动层已经用 mtime/size 判定"文件没变"，这里只是把它计入跳过数，
			// 让界面上能区分"真的扫了"和"压根没动"。
			if ev.Type == storage.EventSkipped {
				job.Skipped++
			}
		}
	}
	flush()
	flushCache()

	// 累加本次命中次数，界面上能看到缓存到底省了多少事
	for i := 0; i < len(hits); i += 500 {
		end := i + 500
		if end > len(hits) {
			end = len(hits)
		}
		m.db.Model(&model.ScanCache{}).
			Where("library_id = ? AND rel_path IN ?", req.LibraryID, hits[i:end]).
			UpdateColumn("hit_count", gorm.Expr("hit_count + 1"))
	}

	// 缓存里已经不存在的路径（文件被移走/改名后残留）顺手清掉，避免表无限膨胀。
	// 只清本次全量扫描覆盖到的库，且同样要求没有失败条目。
	if req.Full && job.Failed == 0 {
		var cached []string
		m.db.Model(&model.ScanCache{}).Where("library_id = ?", req.LibraryID).
			Pluck("rel_path", &cached)
		stale := make([]string, 0, 8)
		for _, p := range cached {
			if _, ok := cacheSeen[p]; !ok {
				stale = append(stale, p)
			}
		}
		for i := 0; i < len(stale); i += 500 {
			end := i + 500
			if end > len(stale) {
				end = len(stale)
			}
			m.db.Where("library_id = ? AND rel_path IN ?", req.LibraryID, stale[i:end]).
				Delete(&model.ScanCache{})
		}
		if len(stale) > 0 {
			log.Printf("[scanner] 库 %d 清理失效扫描缓存 %d 条", req.LibraryID, len(stale))
		}
	}

	// 全量扫描收尾：回收磁盘上已消失的文件索引。
	// 不做的话，用户在宿主机删掉原图后，相册里会永远留着打不开的幽灵条目。
	if req.Full {
		if job.Failed > 0 {
			// 本次扫描有条目失败，说明遍历结果可能不完整，此时回收会误删
			log.Printf("[scanner] 库 %d 本次扫描存在 %d 条错误，跳过失效索引回收", req.LibraryID, job.Failed)
		} else {
			job.Missing += purgeGone(m.db, req.LibraryID, seen)
		}
	}

	// 统计有效文件数并回写库信息
	var cnt int64
	m.db.Model(&model.MediaFile{}).
		Where("library_id = ? AND index_status = ? AND deleted_at IS NULL", req.LibraryID, model.IndexNormal).
		Count(&cnt)
	m.db.Model(&model.Library{}).Where("id = ?", req.LibraryID).
		Updates(map[string]any{"media_count": cnt, "status": 1})
	m.db.Model(&model.MountDir{}).Where("library_id = ?", req.LibraryID).
		Updates(map[string]any{
			"last_scan_at":      time.Now(),
			"last_scan_cost_ms": int(time.Since(start).Milliseconds()),
			"file_count":        cnt,
			"scan_status":       1,
			"error_message":     "",
		})

	_ = lib
	m.finishJob(&job, "")
}

// purgeGone 全量扫描的收尾回收：磁盘上已不存在、但索引里还活着的条目，一律软删。
// 全量扫描遍历了整个目录，所以"没扫到"就等价于"文件没了"。
// 安全策略（宁可不删，也不能误删）：一个文件都没扫到 → 判定为异常情况，直接跳过。
// 调用方需额外保证本次扫描没有失败条目（job.Failed == 0）。
// 注意：不要用"待回收数 > 扫到数"当判据——用户删掉大半照片时这是完全正常的场景。
func purgeGone(db *gorm.DB, libID int64, seen map[string]struct{}) int {
	if len(seen) == 0 {
		return 0
	}
	var alive []string
	if err := db.Model(&model.MediaFile{}).
		Where("library_id = ? AND deleted_at IS NULL", libID).
		Pluck("relative_path", &alive).Error; err != nil {
		log.Printf("[scanner] 库 %d 回收检查失败: %v", libID, err)
		return 0
	}
	gone := make([]string, 0, 8)
	for _, p := range alive {
		if _, ok := seen[p]; !ok {
			gone = append(gone, p)
		}
	}
	if len(gone) == 0 {
		return 0
	}
	now := time.Now()
	const step = 500
	for i := 0; i < len(gone); i += step {
		end := i + step
		if end > len(gone) {
			end = len(gone)
		}
		db.Model(&model.MediaFile{}).
			Where("library_id = ? AND relative_path IN ? AND deleted_at IS NULL", libID, gone[i:end]).
			Update("deleted_at", now)
	}
	log.Printf("[scanner] 库 %d 已回收 %d 条失效索引（源文件已不在磁盘上）", libID, len(gone))
	return len(gone)
}

func (m *Manager) finishJob(job *model.ScanJob, errMsg string) {
	now := time.Now()
	job.FinishedAt = &now
	if errMsg != "" {
		job.Status = 4
		job.Error = truncate(errMsg, 500)
		m.logf(job.ID, 3, "", errMsg)
	} else {
		job.Status = 3
	}
	// 注意：scan_jobs 表没有 updated_at 列，这里不做本地赋值
	_ = m.db.Model(&model.ScanJob{}).Where("id = ?", job.ID).
		Updates(map[string]any{
			"status": job.Status, "scanned": job.Scanned, "added": job.Added,
			"skipped": job.Skipped, "missing": job.Missing, "failed": job.Failed,
			"error": job.Error, "finished_at": job.FinishedAt,
		}).Error
	// 扫描可能新增/回收了大量索引，缓存必须作废，否则用户会看到旧列表
	cache.Invalidate(cache.PfxMedia)
	cache.Invalidate(cache.PfxLibrary)
	cache.Invalidate(cache.PfxAlbum)
}

func (m *Manager) logf(jobID int64, level int8, path, msg string) {
	now := time.Now()
	_ = m.db.Create(&model.ScanLog{
		JobID: jobID, Level: level, Path: truncate(path, 1000),
		Message: truncate(msg, 1000), CreatedAt: &now,
	}).Error
}

// waitCPU CPU 守护：负载过高时主动退避，ARM 低配设备救星
func (m *Manager) waitCPU() {
	if !m.cfg.Scan.CPUGuard {
		return
	}
	limit := float64(m.cfg.Scan.CPULimit) / 100.0
	if limit <= 0 {
		limit = 1.5
	}
	max := float64(runtime.NumCPU())
	for i := 0; i < 60; i++ {
		if load, ok := loadAvg1(); !ok || load < max*limit {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// ---------- 挂载目录变更监听调度（定时启动/重启） ----------

// WatchScheduler 为每个挂载库维护一个常驻变更监听协程
type WatchScheduler struct {
	db      *gorm.DB
	drivers *storage.Registry
	mgr     *Manager // 变更事件最终要落到扫描任务上，否则事件收了也没用
	cancels map[int64]context.CancelFunc
	mu      sync.Mutex
}

func NewWatchScheduler(db *gorm.DB, drivers *storage.Registry, mgr *Manager) *WatchScheduler {
	return &WatchScheduler{db: db, drivers: drivers, mgr: mgr, cancels: map[int64]context.CancelFunc{}}
}

// Start 启动全部挂载库的变更监听（inotify 优先，自动降级轮询）
func (w *WatchScheduler) Start(parent context.Context) {
	var dirs []model.MountDir
	if err := w.db.Where("deleted_at IS NULL").Find(&dirs).Error; err != nil {
		return
	}
	for _, d := range dirs {
		w.StartOne(parent, d)
	}
}

// StartOne 启动单个库的监听
func (w *WatchScheduler) StartOne(parent context.Context, d model.MountDir) {
	w.mu.Lock()
	if _, ok := w.cancels[d.LibraryID]; ok {
		w.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancels[d.LibraryID] = cancel
	w.mu.Unlock()

	go func() {
		drv, ok := w.drivers.Get(d.LibraryID)
		if !ok {
			return
		}
		mdrv, ok := drv.(*storage.MountLocalDirDriver)
		if !ok {
			return
		}
		ch, err := mdrv.Watch(ctx)
		if err != nil {
			log.Printf("[watch] 库 %d 监听启动失败: %v", d.LibraryID, err)
			return
		}
		log.Printf("[watch] 库 %d 变更监听已启动 (inotify=%v)", d.LibraryID, !mdrv.Degraded())

		// 兜底周期扫描：Docker bind mount 下的 inotify 并不总能把宿主机侧的事件
		// 传进容器（取决于存储驱动），实测 -v /photos:/photos:ro 就收不到 Create 事件。
		// 因此无论 inotify 是否可用，都按周期兜底跑一次增量扫描，保证照片一定会入库。
		fallback := d.PollIntervalSec
		if fallback < 60 {
			fallback = 60
		}
		fbTicker := time.NewTicker(time.Duration(fallback) * time.Second)
		defer fbTicker.Stop()

		var timer *time.Timer
		var tmu sync.Mutex
		submit := func() {
			if w.mgr == nil {
				return
			}
			if err := w.mgr.Submit(JobRequest{
				LibraryID: d.LibraryID, Full: false, TriggerSrc: 3,
			}); err != nil {
				if !errors.Is(err, ErrJobRunning) {
					log.Printf("[watch] 库 %d 增量扫描提交失败: %v", d.LibraryID, err)
				}
				return
			}
			log.Printf("[watch] 库 %d 检测到目录变更，已提交增量扫描", d.LibraryID)
		}

		for {
			select {
			case _, ok := <-ch:
				if !ok {
					return
				}
				// 防抖：整批拷照片时合并成一次扫描
				tmu.Lock()
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(3*time.Second, submit)
				tmu.Unlock()
			case <-fbTicker.C:
				submit()
			}
		}
	}()
}

// StopOne 停止单个库监听
func (w *WatchScheduler) StopOne(libraryID int64) {
	w.mu.Lock()
	if c, ok := w.cancels[libraryID]; ok {
		c()
		delete(w.cancels, libraryID)
	}
	w.mu.Unlock()
}

// ---------- 工具 ----------

type rateLimiter struct {
	mu   sync.Mutex
	rate int
	last time.Time
}

func newRateLimiter(ratePerSec int) *rateLimiter {
	if ratePerSec <= 0 {
		ratePerSec = 5
	}
	return &rateLimiter{rate: ratePerSec}
}

// wait 简单令牌桶：限制每秒批量入库次数，防止 IO/CPU 突刺
func (r *rateLimiter) wait() {
	r.mu.Lock()
	defer r.mu.Unlock()
	interval := time.Second / time.Duration(r.rate)
	if wait := interval - time.Since(r.last); wait > 0 {
		time.Sleep(wait)
	}
	r.last = time.Now()
}

func boolToType(full bool) int8 {
	if full {
		return 2
	}
	return 1
}

func sourceTypeOf(d storage.StorageDriver) int8 {
	if d.Kind() == storage.KindManaged {
		return model.SourceManaged
	}
	return model.SourceMounted
}

func timePtr(t time.Time) *time.Time { return &t }

func base(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == os.PathSeparator {
			return p[i+1:]
		}
	}
	return p
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
