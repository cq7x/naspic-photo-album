// Command naspic-server 私有云相册后端服务入口
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"gorm.io/gorm"

	"github.com/naspic/naspic/internal/api"
	"github.com/naspic/naspic/internal/cache"
	"github.com/naspic/naspic/internal/config"
	"github.com/naspic/naspic/internal/ai"
	"github.com/naspic/naspic/internal/model"
	"github.com/naspic/naspic/internal/scanner"
	"github.com/naspic/naspic/internal/storage"
	"github.com/naspic/naspic/internal/store"
	"github.com/naspic/naspic/internal/thumb"
)

func main() {
	cfgPath := flag.String("c", "config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Printf("Naspic 启动中… 版本=%s 档位=%s 数据库=%s",
		api.Version, config.DetectTier(), cfg.Database.Driver)

	db, err := store.Init(cfg)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	if err := api.EnsureDefaultAdmin(db); err != nil {
		log.Printf("创建默认管理员失败: %v", err)
	} else {
		log.Println("提示：默认管理员 admin / naspic123，请首次登录后立即修改密码")
	}
	api.EnsureDefaultAlbums(db)

	// Redis 只读缓存（可选：连不上就自动降级为不缓存，绝不影响主流程）
	cacheSvc, _ := cache.New(cache.Config{
		Enabled:  cfg.Cache.Enabled,
		Addr:     cfg.Cache.Addr,
		Password: cfg.Cache.Password,
		DB:       cfg.Cache.DB,
		Prefix:   cfg.Cache.Prefix,
		TTLSec:   cfg.Cache.TTLSec,
	})
	if cacheSvc != nil {
		cache.SetGlobal(cacheSvc)
		defer cacheSvc.Close()
	}

	drivers := storage.NewRegistry()
	bootstrapDrivers(cfg, db, drivers)

	// 缩略图服务（-tags vips 时使用 libvips，否则自动降级纯 Go 后端）
	storage.RegisterThumbProvider(thumb.New(db, cfg))

	// AI 服务（默认关闭；未带 -tags ai 编译时自动停用，不影响启动）
	if aiSvc := ai.New(db, cfg); aiSvc != nil {
		ai.SetPathResolver(func(m model.MediaFile) (string, error) {
			d, ok := drivers.Get(m.LibraryID)
			if !ok {
				return "", errors.New("驱动未注册")
			}
			return storage.SafeJoin(d.Root(), m.RelativePath)
		})
		defer aiSvc.Stop()
	}

	scanMgr := scanner.NewManager(db, cfg, drivers)
	// 把 scanMgr 交给 watch，目录变更事件才能转成实际的增量扫描任务
	watch := scanner.NewWatchScheduler(db, drivers, scanMgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch.Start(ctx)

	// 定时增量扫描兜底：即使 inotify 可用，也按周期做一次全库轻量增量
	go periodicScan(ctx, db, scanMgr, cfg)

	srv := api.New(cfg, db, drivers, scanMgr, watch)
	srv.AttachCache(cacheSvc)
	httpSrv := &http.Server{
		Addr:         cfg.Server.Host + ":" + strconv.Itoa(cfg.Server.Port),
		Handler:      srv.Engine(),
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
	}

	go func() {
		log.Printf("HTTP 监听 %s", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务异常退出: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("正在停止服务…")
	cancel()
	scanMgr.Stop()
	shutdownCtx, sCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	log.Println("已停止")
}

// bootstrapDrivers 启动时按数据库配置注册各库驱动
func bootstrapDrivers(cfg *config.Config, db *gorm.DB, drivers *storage.Registry) {
	var libs []model.Library
	if err := db.Where("deleted_at IS NULL").Find(&libs).Error; err != nil {
		log.Printf("[boot] 读取相册库失败: %v", err)
		return
	}
	for _, lib := range libs {
		if lib.Type == model.LibraryTypeMounted {
			var md model.MountDir
			if err := db.Where("library_id = ? AND deleted_at IS NULL", lib.ID).First(&md).Error; err != nil {
				log.Printf("[boot] 库 %d 缺少挂载配置，已跳过", lib.ID)
				continue
			}
			var ignore, include []string
			_ = json.Unmarshal([]byte(md.IgnoreRules), &ignore)
			_ = json.Unmarshal([]byte(md.IncludeExts), &include)
			d, err := storage.NewMountDriver(storage.MountConfig{
				LibraryID: lib.ID, HostPath: md.HostPath, Mode: md.Mode, AllowDelete: md.AllowDelete == 1,
				WatchMode: md.WatchMode, PollInterval: md.PollIntervalSec,
				Recursive: md.Recursive == 1, IgnoreRules: ignore, IncludeExts: include,
				HashFullMax: int64(cfg.Sync.HashFullMaxBytes),
			})
			if err != nil {
				log.Printf("[boot] 库 %d 挂载驱动初始化失败: %v", lib.ID, err)
				continue
			}
			drivers.Register(lib.ID, d)
			log.Printf("[boot] 挂载库 %d 已就绪 path=%s mode=%d", lib.ID, md.HostPath, md.Mode)
			continue
		}
		d, err := storage.NewLocalDriver(lib.ID, lib.StorageRoot, int64(cfg.Sync.HashFullMaxBytes))
		if err != nil {
			log.Printf("[boot] 库 %d 托管驱动初始化失败: %v", lib.ID, err)
			continue
		}
		drivers.Register(lib.ID, d)
	}
}

// periodicScan 定时对所有库做一次增量扫描（低频、限流，ARM 友好）
func periodicScan(ctx context.Context, db *gorm.DB, mgr *scanner.Manager, cfg *config.Config) {
	interval := time.Duration(cfg.Scan.PollInterval) * time.Second
	if interval < time.Minute {
		interval = 5 * time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			var libs []model.Library
			if err := db.Where("deleted_at IS NULL AND status = 1").Find(&libs).Error; err != nil {
				continue
			}
			for _, l := range libs {
				// 队列满或正在扫描时静默跳过，下一轮再试
				_ = mgr.Submit(scanner.JobRequest{LibraryID: l.ID, Full: false, TriggerSrc: 2})
			}
		}
	}
}
