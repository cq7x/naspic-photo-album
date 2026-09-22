// Package api 数据库安装向导(setup mode)。
//
// 当 cfg.Database.DSN 为空时,主程序不连 DB,只挂载本文件提供的
// SetupServer,让前端通过 Web 表单填写数据库参数 → 测试连接 → 保存。
// 保存成功后通过 onInstall 回调触发热加载,切换到完整业务引擎。
package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	glogger "gorm.io/gorm/logger"

	"github.com/naspic/naspic/internal/config"
)

// SetupServer 安装向导服务(数据库未配置或连接失败时启用)
type SetupServer struct {
	cfg       *config.Config
	cfgPath   string
	onInstall func(*config.Config) error
	bootError string // 降级时的错误信息(DSN 非空但连不上)
}

// NewSetupServer 构造安装向导服务
func NewSetupServer(cfg *config.Config, cfgPath string, onInstall func(*config.Config) error) *SetupServer {
	return &SetupServer{cfg: cfg, cfgPath: cfgPath, onInstall: onInstall}
}

// SetBootError 设置降级原因(bootFull 失败时由 main 调用)
func (s *SetupServer) SetBootError(msg string) {
	s.bootError = msg
}

// Engine 构建 setup 模式的 Gin 引擎
func (s *SetupServer) Engine() *gin.Engine {
	gin.SetMode(s.cfg.Server.Mode)
	r := gin.New()
	r.Use(gin.Recovery(), loggerMiddleware())
	r.MaxMultipartMemory = 16 << 20

	r.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true, "version": Version, "time": time.Now().UTC()})
	})

	r.GET("/api/v1/setup/status", s.status)
	r.POST("/api/v1/setup/test", s.testConn)
	r.POST("/api/v1/setup/save", s.save)

	// 托管前端(若可用),便于浏览器直接打开首页就落到 setup 路由
	s.ensureWebRoot()
	s.mountWeb(r)
	return r
}

// setupForm 前端表单
type setupForm struct {
	Host     string `json:"host" binding:"required"`
	Port     int    `json:"port" binding:"required"`
	DB       string `json:"db" binding:"required"`
	User     string `json:"user" binding:"required"`
	Password string `json:"password"`
}

// statusResp GET /setup/status 返回
type statusResp struct {
	NeedSetup bool       `json:"need_setup"`
	BootError string     `json:"boot_error,omitempty"`
	Defaults  setupForm  `json:"defaults"`
}

// status 返回当前是否需要安装 + 降级错误信息 + MySQL 默认值供前端预填
func (s *SetupServer) status(c *gin.Context) {
	ok(c, statusResp{
		NeedSetup: s.cfg.Database.DSN == "" || s.bootError != "",
		BootError: s.bootError,
		Defaults: setupForm{
			Host: "mysql", Port: 3306, DB: "naspic", User: "naspic",
		},
	})
}

// testConn 测试数据库连接,不写文件、不调 AutoMigrate
func (s *SetupServer) testConn(c *gin.Context) {
	var f setupForm
	if err := c.ShouldBindJSON(&f); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	dsn := buildMySQLDSN(f)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: glogger.Default.LogMode(glogger.Warn),
	})
	if err != nil {
		fail(c, 400, "连接失败: "+err.Error())
		return
	}
	sqlDB, err := db.DB()
	if err != nil {
		fail(c, 500, "获取底层连接失败: "+err.Error())
		return
	}
	defer sqlDB.Close()

	start := time.Now()
	if err := sqlDB.Ping(); err != nil {
		fail(c, 400, "Ping 失败: "+err.Error())
		return
	}
	latency := time.Since(start).Milliseconds()

	// 取 MySQL 版本(失败不影响 ok)
	var version string
	_ = sqlDB.QueryRow("SELECT VERSION()").Scan(&version)

	ok(c, gin.H{
		"ok":         true,
		"latency_ms": latency,
		"version":    version,
	})
}

// save 保存数据库配置,写回 config.yaml,触发热加载
func (s *SetupServer) save(c *gin.Context) {
	var f setupForm
	if err := c.ShouldBindJSON(&f); err != nil {
		fail(c, 400, "参数错误: "+err.Error())
		return
	}
	dsn := buildMySQLDSN(f)

	// 先测一次连接,避免保存了但连不上
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: glogger.Default.LogMode(glogger.Warn),
	})
	if err != nil {
		fail(c, 400, "连接失败: "+err.Error())
		return
	}
	if sqlDB, e := db.DB(); e == nil {
		_ = sqlDB.Close()
	}

	// 写回 config.yaml
	if err := config.SaveDatabase(s.cfgPath, "mysql", dsn); err != nil {
		fail(c, 500, "保存配置失败: "+err.Error())
		return
	}

	// 更新内存中的 cfg,触发热加载
	s.cfg.Database.Driver = "mysql"
	s.cfg.Database.DSN = dsn
	if s.onInstall != nil {
		if err := s.onInstall(s.cfg); err != nil {
			// config.yaml 已写入,但热加载失败(如 AutoMigrate 出错)
			// handler 仍是 setup engine,用户可修改参数重试
			fail(c, 500, "数据库初始化失败: "+err.Error()+"（配置已保存,可修改参数重试或重启容器）")
			return
		}
	}

	ok(c, gin.H{
		"ok":    true,
		"admin": gin.H{"username": "admin", "password": "naspic123"},
	})
}

// buildMySQLDSN 拼接 GORM MySQL DSN
func buildMySQLDSN(f setupForm) string {
	host := strings.TrimSpace(f.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	port := f.Port
	if port <= 0 {
		port = 3306
	}
	db := strings.TrimSpace(f.DB)
	if db == "" {
		db = "naspic"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		f.User, f.Password, host, strconv.Itoa(port), db)
}

// ensureWebRoot setup mode 下若 WebRoot 没设置,尝试常见默认值,
// 方便用户在 docker 里运行(默认镜像里前端在 /web)
func (s *SetupServer) ensureWebRoot() {
	if s.cfg.Server.WebRoot != "" {
		return
	}
	for _, p := range []string{"/web", "./web/dist", "web/dist"} {
		if _, err := os.Stat(filepath.Join(p, "index.html")); err == nil {
			s.cfg.Server.WebRoot = p
			return
		}
	}
}

// mountWeb 托管 Web 前端(SPA),逻辑与 Server.mountWeb 一致,
// 复制以避免跨结构体方法调用
func (s *SetupServer) mountWeb(r *gin.Engine) {
	root := strings.TrimSpace(s.cfg.Server.WebRoot)
	if root == "" {
		return
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return
	}
	index := filepath.Join(root, "index.html")
	if _, err := os.Stat(index); err != nil {
		return
	}
	r.Static("/assets", filepath.Join(root, "assets"))
	r.StaticFile("/favicon.ico", filepath.Join(root, "favicon.ico"))
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(404, resp{Code: 404, Msg: "接口不存在"})
			return
		}
		c.File(index)
	})
}
