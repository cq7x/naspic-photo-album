package api

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/naspic/naspic/internal/cache"
	"github.com/naspic/naspic/internal/media"
	"github.com/naspic/naspic/internal/model"
	"github.com/naspic/naspic/internal/storage"
)

// 补采拍摄时间：历史视频入库时一律用文件 mtime 当拍摄时间（导出/复制过的文件
// mtime 全乱），导致时间轴排序错乱。这个任务对每条视频跑一次 ffprobe，
// 把容器里的 creation_time 采回来，顺带补时长与分辨率。
//
// 只跑 ffprobe 读容器头，不重算哈希也不重新生成缩略图，所以比 force 重扫快得多。
// 任务在后台跑，前端轮询状态即可——一次几千个视频可能要几分钟。

type takenJobState struct {
	Running    bool   `json:"running"`
	Total      int    `json:"total"`
	Done       int    `json:"done"`
	Updated    int    `json:"updated"`
	Skipped    int    `json:"skipped"`
	Error      string `json:"error,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
	FFProbe    bool   `json:"ffprobe"` // 镜像里有没有装 ffprobe
}

var (
	takenMu    sync.Mutex
	takenState takenJobState
)

// POST /api/v1/media/refresh-taken  启动补采任务
func (s *Server) refreshTakenStart(c *gin.Context) {
	// 先探测 ffprobe 是否可用，不可用就别白跑一趟
	if !media.FFProbeAvailable() {
		fail(c, 500, "服务端未安装 ffprobe，无法读取视频容器时间")
		return
	}
	var body struct {
		LibraryID int64 `json:"library_id"`
		Limit     int   `json:"limit"`
		OnlyVideo bool  `json:"only_video"`
	}
	_ = c.ShouldBindJSON(&body)
	if body.Limit <= 0 {
		body.Limit = 5000
	}
	onlyVideo := true
	// 传 false 可以连图片一起补（图片一般已有 EXIF，通常没必要）
	if c.Query("all") == "1" {
		onlyVideo = false
	}
	if !body.OnlyVideo && c.Request.Method == "POST" && c.Query("all") != "1" {
		onlyVideo = false
	}

	takenMu.Lock()
	if takenState.Running {
		takenMu.Unlock()
		fail(c, 409, "补采任务正在运行中，请稍后再试")
		return
	}
	takenState = takenJobState{Running: true, FFProbe: true, StartedAt: time.Now().Format(time.RFC3339)}
	takenMu.Unlock()

	go s.runRefreshTaken(body.LibraryID, body.Limit, onlyVideo)
	ok(c, gin.H{"started": true})
}

// GET /api/v1/media/refresh-taken  查询补采任务状态
func (s *Server) refreshTakenStatus(c *gin.Context) {
	takenMu.Lock()
	st := takenState
	takenMu.Unlock()
	st.FFProbe = media.FFProbeAvailable()
	ok(c, st)
}

func (s *Server) runRefreshTaken(libraryID int64, limit int, onlyVideo bool) {
	set := func(f func(*takenJobState)) {
		takenMu.Lock()
		f(&takenState)
		takenMu.Unlock()
	}

	// 1. 挑出候选：还没补过容器时间的（taken_at_source=2 表示用的是文件 mtime）
	// Pluck 必须显式给 Model，否则 GORM 不知道查哪张表
	q := s.db.Model(&model.MediaFile{}).
		Where("deleted_at IS NULL").
		Where("taken_at_source = ?", media.TakenSourceMtime)
	if onlyVideo {
		q = q.Where("media_type = ?", 2)
	}
	if libraryID > 0 {
		q = q.Where("library_id = ?", libraryID)
	}
	var ids []int64
	if err := q.Limit(limit).Pluck("id", &ids).Error; err != nil {
		set(func(t *takenJobState) {
			t.Running = false
			t.Error = err.Error()
			t.FinishedAt = time.Now().Format(time.RFC3339)
		})
		return
	}

	set(func(t *takenJobState) { t.Total = len(ids) })

	var m model.MediaFile
	for _, id := range ids {
		if err := s.db.First(&m, id).Error; err != nil {
			set(func(t *takenJobState) { t.Done++; t.Skipped++ })
			continue
		}
		func() {
			d, exist := s.drivers.Get(m.LibraryID)
			if !exist || d.Root() == "" { // 远端驱动没有本地路径，ffprobe 读不到
				set(func(t *takenJobState) { t.Done++; t.Skipped++ })
				return
			}
			abs, jerr := storage.SafeJoin(d.Root(), m.RelativePath)
			if jerr != nil {
				set(func(t *takenJobState) { t.Done++; t.Skipped++ })
				return
			}
			meta, merr := media.Parse(abs)
			if merr != nil || meta == nil || meta.TakenSource == media.TakenSourceMtime {
				// 容器里确实没有时间信息，保持原样（mtime 兜底），只补时长尺寸
				if meta != nil && (meta.DurationMs > 0 || meta.Width > 0) {
					upd := map[string]interface{}{}
					if meta.DurationMs > 0 {
						upd["duration_ms"] = meta.DurationMs
					}
					if meta.Width > 0 {
						upd["width"], upd["height"] = meta.Width, meta.Height
					}
					upd["updated_at"] = time.Now()
					_ = s.db.Model(&model.MediaFile{}).Where("id = ?", id).Updates(upd).Error
					set(func(t *takenJobState) { t.Done++; t.Updated++ })
					return
				}
				set(func(t *takenJobState) { t.Done++; t.Skipped++ })
				return
			}
			upd := map[string]interface{}{
				"taken_at":        meta.TakenAt,
				"taken_at_source": meta.TakenSource,
				"updated_at":      time.Now(),
			}
			if meta.DurationMs > 0 {
				upd["duration_ms"] = meta.DurationMs
			}
			if meta.Width > 0 {
				upd["width"], upd["height"] = meta.Width, meta.Height
			}
			if err := s.db.Model(&model.MediaFile{}).Where("id = ?", id).Updates(upd).Error; err != nil {
				set(func(t *takenJobState) { t.Done++; t.Skipped++ })
				return
			}
			set(func(t *takenJobState) { t.Done++; t.Updated++ })
		}()
	}

	// 时间变了，列表/统计缓存全部作废
	s.invalidate(cache.PfxMedia)

	set(func(t *takenJobState) {
		t.Running = false
		t.FinishedAt = time.Now().Format(time.RFC3339)
	})
}
