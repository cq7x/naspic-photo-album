// Package api HTTP 接口层（Gin）
package api

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/naspic/naspic/internal/cache"
	"github.com/naspic/naspic/internal/config"
	"github.com/naspic/naspic/internal/scanner"
	"github.com/naspic/naspic/internal/storage"
	"gorm.io/gorm"
)

// Server API 服务
type Server struct {
	cfg     *config.Config
	db      *gorm.DB
	drivers *storage.Registry
	scanMgr *scanner.Manager
	watch   *scanner.WatchScheduler
	auth    *Auth
	cache   *cache.Service
}

// New 创建 API 服务并注册路由
func New(cfg *config.Config, db *gorm.DB, drivers *storage.Registry, scanMgr *scanner.Manager, watch *scanner.WatchScheduler) *Server {
	return &Server{cfg: cfg, db: db, drivers: drivers, scanMgr: scanMgr, watch: watch, auth: NewAuth(db)}
}

// AttachCache 接入 Redis 缓存（可选，nil 表示不启用）
func (s *Server) AttachCache(c *cache.Service) { s.cache = c }

// invalidate 按前缀清缓存。缓存不可用时为空操作。
func (s *Server) invalidate(prefix string) {
	if s.cache == nil {
		return
	}
	s.cache.DelPrefix(prefix)
}

// Engine 构建 Gin 引擎
func (s *Server) Engine() *gin.Engine {
	gin.SetMode(s.cfg.Server.Mode)
	r := gin.New()
	r.Use(gin.Recovery(), loggerMiddleware())

	// 手机原图/4K 视频动辄几十 MB，内存缓冲放宽到 512MB，超出部分自动落临时盘
	r.MaxMultipartMemory = 512 << 20

	r.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true, "version": Version, "time": time.Now().UTC()})
	})

	// 认证
	r.POST("/api/v1/auth/login", s.auth.login)

	// 以下接口需要登录
	g := r.Group("/api/v1", s.auth.Middleware())
	{
		// 相册库与挂载目录
		g.GET("/libraries", s.listLibraries)
		g.POST("/libraries", s.createLibrary)
		g.POST("/mount-dirs", s.createMountDir)
		g.GET("/mount-dirs", s.listMountDirs)
		g.DELETE("/libraries/:id", s.deleteLibrary)

		// 扫描
		g.POST("/libraries/:id/scan", s.triggerScan)
		g.GET("/libraries/:id/scan/jobs", s.listScanJobs)
		g.GET("/scan/jobs/:id/logs", s.scanJobLogs)

		// 缓存（缩略图 / 扫描指纹，均为可重建缓存）
		g.GET("/libraries/:id/cache", s.libraryCache)
		g.POST("/libraries/:id/cache/clear", s.clearLibraryCache)

		// 权限（挂载目录可授权给多用户/多群组）
		g.GET("/permissions", s.listPermissions)
		g.PUT("/permissions", s.upsertPermission)
		g.DELETE("/permissions/:id", s.deletePermission)
		g.GET("/users", s.listUsers)
		g.GET("/groups", s.listGroups)
		g.POST("/groups", s.createGroup)

		// 管理员账号设置
		g.GET("/admin/users", s.listAdminUsers)
		g.POST("/admin/users", s.createAdminUser)
		g.PUT("/admin/users/:id", s.updateAdminUser)
		g.DELETE("/admin/users/:id", s.deleteAdminUser)
		g.PUT("/admin/users/:id/password", s.resetAdminPassword)
		g.POST("/auth/password", s.changeMyPassword)
		g.GET("/auth/me", s.myProfile)

		// 相册（自建 / 按规则归类，取代旧的「收藏」）
		g.GET("/albums", s.listAlbums)
		g.POST("/albums", s.createAlbum)
		g.PATCH("/albums/:id", s.updateAlbum)
		g.DELETE("/albums/:id", s.deleteAlbum)
		g.GET("/albums/:id/media", s.albumMedia)
		g.POST("/albums/:id/items", s.addAlbumItems)
		g.DELETE("/albums/:id/items", s.removeAlbumItems)
		g.DELETE("/albums/:id/items/:mediaId", s.removeAlbumItems)
		g.POST("/albums/:id/apply", s.applyAlbum)
		g.GET("/media/:id/albums", s.mediaAlbums)

		// 媒体（浏览 / 预览 / 批量）
		g.GET("/media", s.listMedia)
		g.GET("/media/stats", s.mediaStats)
		g.GET("/media/:id/detail", s.getMediaDetail)
		g.GET("/media/:id/file", s.getMediaFile)
		g.GET("/media/:id/thumb", s.getMediaThumb)
		g.PATCH("/media/:id", s.updateMedia)
	g.POST("/media/batch", s.batchMedia)
	g.DELETE("/media/:id", s.deleteMedia)
	// 补采拍摄时间（历史视频用 mtime 排序不准，跑一次 ffprobe 把容器时间读回来）
	g.POST("/media/refresh-taken", s.refreshTakenStart)
	g.GET("/media/refresh-taken", s.refreshTakenStatus)

		// 网页端上传（multipart，仅托管可写库）
		g.POST("/upload/web", s.uploadWeb)

		// 手机同步上传（分片 + 断点续传 + 秒传）
		g.POST("/upload/check", s.uploadCheck)
		g.POST("/upload/init", s.uploadInit)
		g.PUT("/upload/chunk", s.uploadChunk)
		g.POST("/upload/complete", s.uploadComplete)

		// 手机设备/同步任务
		g.POST("/sync/devices", s.registerDevice)
		g.GET("/sync/devices", s.listDevices)
		g.GET("/sync/tasks", s.listSyncTasks)
		g.PUT("/sync/tasks", s.upsertSyncTask)
		g.DELETE("/sync/tasks/:id", s.deleteSyncTask)
		g.GET("/sync/tasks/:id/records", s.listSyncRecords)
	}

	// 前端静态资源（放最后注册，避免抢占 /api 路由）
	s.mountWeb(r)
	return r
}

// mountWeb 托管 Web 前端（SPA）。
// 目录缺失或没有 index.html 时静默降级为纯 API 模式，不影响服务启动。
func (s *Server) mountWeb(r *gin.Engine) {
	root := strings.TrimSpace(s.cfg.Server.WebRoot)
	if root == "" {
		return
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		log.Printf("[web] 前端目录不可用（%s），以纯 API 模式运行", root)
		return
	}
	index := filepath.Join(root, "index.html")
	if _, err := os.Stat(index); err != nil {
		log.Printf("[web] %s 下缺少 index.html，以纯 API 模式运行", root)
		return
	}

	r.Static("/assets", filepath.Join(root, "assets"))
	r.StaticFile("/favicon.ico", filepath.Join(root, "favicon.ico"))
	// SPA 回落：非 /api 请求一律返回 index.html，交给前端路由处理
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(404, resp{Code: 404, Msg: "接口不存在"})
			return
		}
		c.File(index)
	})
	log.Printf("[web] 前端已挂载 %s", root)
}

// Version 服务版本（构建时可用 -ldflags 覆盖）
var Version = "0.3.0"

// loggerMiddleware 访问日志。
// 注意：静态资源（/assets、/favicon、缩略图）量太大，只记录异常状态码，避免刷屏。
func loggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		code := c.Writer.Status()
		p := c.Request.URL.Path

		// 缩略图/原图会按 304 命中缓存，属于正常流量，静音
		isAsset := strings.HasPrefix(p, "/assets/") ||
			strings.HasPrefix(p, "/favicon") ||
			strings.Contains(p, "/thumb") ||
			strings.Contains(p, "/media/")

		if isAsset && code < 400 {
			return
		}

		latency := time.Since(start)
		if code >= 400 {
			var msg string
			if len(c.Errors) > 0 {
				msg = " err=" + c.Errors.String()
			}
			log.Printf("[http] %-6s %s %d %v%s ua=%s",
				c.Request.Method, p, code, latency.Round(time.Millisecond), msg,
				shortUA(c.Request.UserAgent()))
			return
		}
		log.Printf("[http] %-6s %s %d %v", c.Request.Method, p, code, latency.Round(time.Millisecond))
	}
}

// shortUA 只保留 UA 前 40 字符，避免日志被长 UA 撑爆
func shortUA(ua string) string {
	if len(ua) > 40 {
		return ua[:40]
	}
	return ua
}

type resp struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg,omitempty"`
	Data interface{} `json:"data,omitempty"`
}

func ok(c *gin.Context, data interface{})          { c.JSON(200, resp{Code: 0, Data: data}) }
func fail(c *gin.Context, status int, msg string)  { c.JSON(status, resp{Code: status, Msg: msg}) }
