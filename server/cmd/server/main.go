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
	"sync/atomic"
	"syscall"
	"time"

	"gorm.io/gorm"

	"github.com/naspic/naspic/internal/ai"
	"github.com/naspic/naspic/internal/api"
	"github.com/naspic/naspic/internal/cache"
	"github.com/naspic/naspic/internal/config"
	"github.com/naspic/naspic/internal/model"
	"github.com/naspic/naspic/internal/scanner"
	"github.com/naspic/naspic/internal/storage"
	"github.com/naspic/naspic/internal/store"
	"github.com/naspic/naspic/internal/thumb"
)

// handler 在 setup / full 模式之间原子切换 HTTP handler,无需重启容器
var handler atomic.Value // 存 http.Handler

func main() {
	cfgPath := flag.String("c", "config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Printf("Naspic 启动中… 版本=%s 档位=%s 数据库=%s",
		api.Version, config.DetectTier(), cfg.Database.Driver)

	// onInstall: setup 模式下,用户保存数据库配置后触发的热加载回调
	onInstall := func(newCfg *config.Config) error {
		return bootFull(newCfg, *cfgPath)
	}

	// 启动逻辑:无论数据库状态如何,网站都要能打开
	// 1) DSN 为空 → setup 模式(首次部署)
	// 2) DSN 非空但连接失败 → 降级到 setup 模式(显示错误,让用户重新填)
	// 3) DSN 非空且连接成功 → 完整引擎
	var bootError string
	if cfg.Database.DSN != "" {
		if err := bootFull(cfg, *cfgPath); err != nil {
			bootError = err.Error()
			log.Printf("[boot] 数据库连接失败,降级到安装模式: %v", err)
		}
	}
	if bootError != "" || cfg.Database.DSN == "" {
		if cfg.Database.DSN == "" {
			log.Printf("[boot] 数据库未配置,进入安装模式")
		}
		setupSrv := api.NewSetupServer(cfg, *cfgPath, onInstall)
		setupSrv.SetBootError(bootError)
		handler.Store(setupSrv.Engine())
	}

	// HTTP 服务(共享 handler,setup → full 切换靠 atomic.Value)
	httpSrv := &http.Server{
		Addr:         cfg.Server.Host + ":" + strconv.Itoa(cfg.Server.Port),
		Handler:      http.HandlerFunc(serveHTTP),
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
	shutdownCtx, sCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	log.Println("已停止")
}

// serveHTTP 从 atomic.Value 取当前 handler 处理请求
func serveHTTP(w http.ResponseWriter, r *http.Request) {
	h, ok := handler.Load().(http.Handler)
	if !ok || h == nil {
		http.Error(w, "服务未就绪", http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, r)
}

// bootFull 执行完整启动序列:连 DB → 建管理员 → 注册驱动 → 缩略图/AI → 扫描 → 完整引擎,
// 成功后把 engine 原子切换到 handler。setup 模式热加载和正常启动共用此函数。
// 失败时若为正常启动返回错误让 main fatal,setup 模式下返回错误让前端显示。
func bootFull(cfg *config.Config, cfgPath string) error {
	db, err := store.Init(cfg)
	if err != nil {
		return err
	}
	if err := api.EnsureDefaultAdmin(db); err != nil {
		log.Printf("创建默认管理员失败: %v", err)
	} else {
		log.Println("提示：默认管理员 admin / naspic123，请首次登录后立即修改密码")
	}
	api.EnsureDefaultAlbums(db)

	// Redis 只读缓存(可选)
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
	}

	drivers := storage.NewRegistry()
	bootstrapDrivers(cfg, db, drivers)
	storage.RegisterThumbProvider(thumb.New(db, cfg))

	if aiSvc := ai.New(db, cfg); aiSvc != nil {
		ai.SetPathResolver(func(m model.MediaFile) (string, error) {
			d, ok := drivers.Get(m.LibraryID)
			if !ok {
				return "", errors.New("驱动未注册")
			}
			return storage.SafeJoin(d.Root(), m.RelativePath)
		})
		// 注意:不在此 defer aiSvc.Stop() —— bootFull 在 setup→full 热加载时
		// 也会被调用,defer 会在函数返回时关掉 AI 服务,但完整引擎还需要它。
		// AI 服务会随进程退出自然清理。
	}

	scanMgr := scanner.NewManager(db, cfg, drivers)
	watch := scanner.NewWatchScheduler(db, drivers, scanMgr)

	// 用 context.Background():后台任务(watch/periodicScan)随进程退出自然清理,
	// 不需要 cancel。setup→full 热加载只调用一次 bootFull,不会启动多份 goroutine。
	ctx := context.Background()
	watch.Start(ctx)
	go periodicScan(ctx, db, scanMgr, cfg)

	srv := api.New(cfg, db, drivers, scanMgr, watch)
	srv.AttachCache(cacheSvc)
	handler.Store(srv.Engine())
	log.Printf("[boot] 完整引擎已就绪,热加载完成")
	return nil
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
