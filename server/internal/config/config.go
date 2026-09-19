package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Tier 资源档位，由内存自动推导，也可在配置里显式覆盖
type Tier string

const (
	TierLow  Tier = "low"
	TierMid  Tier = "mid"
	TierHigh Tier = "high"
)

// Config 全局配置
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Cache    CacheConfig    `yaml:"cache"`
	Storage  StorageConfig  `yaml:"storage"`
	Scan     ScanConfig     `yaml:"scan"`
	Thumb    ThumbConfig    `yaml:"thumb"`
	AI       AIConfig       `yaml:"ai"`
	Sync     SyncConfig     `yaml:"sync"`
	Log      LogConfig      `yaml:"log"`
}

type ServerConfig struct {
	Host         string `yaml:"host"`          // 监听地址，默认 0.0.0.0
	Port         int    `yaml:"port"`          // 默认 8080
	Mode         string `yaml:"mode"`          // debug / release
	DataDir      string `yaml:"data_dir"`      // 数据根目录：数据库、缓存、临时文件
	WebRoot      string `yaml:"web_root"`      // Web 前端静态文件目录，留空则不托管前端
	TimeZone     string `yaml:"timezone"`      // 默认 Asia/Shanghai
	ReadTimeout  int    `yaml:"read_timeout"`  // 秒
	WriteTimeout int    `yaml:"write_timeout"` // 秒
}

type DatabaseConfig struct {
	Driver   string `yaml:"driver"`    // sqlite / mysql
	DSN      string `yaml:"dsn"`       // sqlite 路径 或 mysql dsn
	MaxOpen  int    `yaml:"max_open"`
	MaxIdle  int    `yaml:"max_idle"`
	LogLevel string `yaml:"log_level"` // silent / error / warn / info
}

// CacheConfig Redis 只读缓存。Enabled=false 或连不上时自动降级为「不缓存」，
// 绝不因为缓存拖垮相册主流程。
type CacheConfig struct {
	Enabled  bool   `yaml:"enabled"`   // 是否启用；关掉就是纯回源
	Addr     string `yaml:"addr"`      // 127.0.0.1:6379
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	Prefix   string `yaml:"prefix"`    // key 前缀，多实例共用 Redis 时区分
	TTLSec   int    `yaml:"ttl_sec"`   // 默认 60 秒
}

type StorageConfig struct {
	ManagedRoot string `yaml:"managed_root"` // 托管存储根目录
	CacheRoot   string `yaml:"cache_root"`   // 缩略图缓存根目录（与原图分离）
	TempRoot    string `yaml:"temp_root"`    // 分片上传暂存目录
}

type ScanConfig struct {
	Workers      int  `yaml:"workers"`        // 扫描并发，ARM 低配建议 1~2
	QueueSize    int  `yaml:"queue_size"`     // 有界队列容量
	RatePerSec   int  `yaml:"rate_per_sec"`   // 令牌桶速率，防止 CPU 打满
	BatchSize    int  `yaml:"batch_size"`     // 每批入库条数
	PollInterval int  `yaml:"poll_interval"`  // 轮询周期（秒）
	EnableWatch  bool `yaml:"enable_watch"`   // 是否启用 inotify
	CPUGuard     bool `yaml:"cpu_guard"`      // CPU 负载守护，超阈值自动退避
	CPULimit     int  `yaml:"cpu_limit"`      // 负载阈值（百分比），如 150 表示 1.5*CPU数
}

type ThumbConfig struct {
	Enabled   bool   `yaml:"enabled"`    // 关闭时使用纯 Go 降级实现
	Backend   string `yaml:"backend"`    // vips / go
	Workers   int    `yaml:"workers"`    // ARM 建议 1~2
	QueueSize int    `yaml:"queue_size"`
	Sizes     string `yaml:"sizes"`      // 如 "256,512,1080"
	Quality   int    `yaml:"quality"`    // WebP/JPEG 质量
	Format    string `yaml:"format"`     // webp / jpeg
}

type AIConfig struct {
	Enabled    bool   `yaml:"enabled"`
	ModelDir   string `yaml:"model_dir"`
	Workers    int    `yaml:"workers"`
	MaxMemory  int    `yaml:"max_memory"`  // MB
	CPUWeight  int    `yaml:"cpu_weight"`  // 0~100
	DetectFace bool   `yaml:"detect_face"`
	SceneModel string `yaml:"scene_model"`
}

type SyncConfig struct {
	ChunkSizeWifi    int `yaml:"chunk_size_wifi"`    // 字节，默认 4MB
	ChunkSizeMobile  int `yaml:"chunk_size_mobile"`  // 默认 1MB
	SessionTTLHours  int `yaml:"session_ttl_hours"`  // 默认 168（7 天）
	MaxRetry         int `yaml:"max_retry"`          // 默认 5
	HashFullMaxBytes int `yaml:"hash_full_max_bytes"` // 小于该值全量哈希，默认 20MB
}

type LogConfig struct {
	Level      string `yaml:"level"`       // debug/info/warn/error
	File       string `yaml:"file"`        // 空=仅 stdout
	MaxSizeMB  int    `yaml:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups"`
}

// DetectTier 按可用内存推导资源档位（ARM 低配自动降档）
func DetectTier() Tier {
	if v := os.Getenv("NASPIC_TIER"); v != "" {
		return Tier(v)
	}
	gb := totalMemoryGB()
	switch {
	case gb <= 0:
		return TierMid // 探测失败取中档，保守
	case gb <= 1:
		return TierLow
	case gb <= 4:
		return TierMid
	default:
		return TierHigh
	}
}

// Default 返回带档位优化的默认配置
func Default() *Config {
	c := &Config{
		Server: ServerConfig{
			Host: "0.0.0.0", Port: 8080, Mode: "release",
			DataDir: "./data", WebRoot: "/web", TimeZone: "Asia/Shanghai",
			ReadTimeout: 60, WriteTimeout: 300,
		},
		Database: DatabaseConfig{
			Driver: "sqlite", DSN: "./data/naspic.db",
			MaxOpen: 10, MaxIdle: 2, LogLevel: "warn",
		},
		// 默认关闭：没部署 Redis 时不能有任何副作用
		Cache: CacheConfig{
			Enabled: false, Addr: "127.0.0.1:6379", DB: 0,
			Prefix: "naspic:", TTLSec: 60,
		},
		Storage: StorageConfig{
			ManagedRoot: "./data/managed",
			CacheRoot:   "./data/cache",
			TempRoot:    "./data/tmp",
		},
		Scan: ScanConfig{
			Workers: 2, QueueSize: 512, RatePerSec: 5, BatchSize: 300,
			PollInterval: 300, EnableWatch: true, CPUGuard: true, CPULimit: 150,
		},
		Thumb: ThumbConfig{
			Enabled: true, Backend: "vips", Workers: 2, QueueSize: 256,
			Sizes: "256,512,1080", Quality: 82, Format: "webp",
		},
		AI: AIConfig{
			Enabled: false, ModelDir: "./data/models", Workers: 1,
			MaxMemory: 512, CPUWeight: 20, DetectFace: true, SceneModel: "mobilenet_v3_small",
		},
		Sync: SyncConfig{
			ChunkSizeWifi: 4 << 20, ChunkSizeMobile: 1 << 20,
			SessionTTLHours: 168, MaxRetry: 5, HashFullMaxBytes: 20 << 20,
		},
		Log: LogConfig{Level: "info", MaxSizeMB: 32, MaxBackups: 5},
	}
	c.ApplyTier(DetectTier())
	return c
}

// ApplyTier 按档位覆盖资源相关默认值
func (c *Config) ApplyTier(t Tier) {
	switch t {
	case TierLow:
		c.Database.Driver = "sqlite"
		c.Database.MaxOpen = 4
		c.Scan.Workers, c.Scan.RatePerSec, c.Scan.BatchSize = 1, 2, 100
		c.Scan.PollInterval, c.Scan.EnableWatch = 600, false
		c.Thumb.Workers, c.Thumb.Backend = 1, "vips"
		c.AI.Enabled = false
	case TierMid:
		c.Scan.Workers, c.Scan.RatePerSec, c.Scan.BatchSize = 2, 4, 200
		c.Thumb.Workers = 2
		c.AI.Enabled = false // 中档默认仍关闭，由用户在 Web 端开启
	case TierHigh:
		c.Scan.Workers = minInt(4, runtime.NumCPU()/2)
		if c.Scan.Workers < 2 {
			c.Scan.Workers = 2
		}
		c.Scan.RatePerSec = 10
		c.Thumb.Workers = minInt(4, runtime.NumCPU()/2)
	}
}

// Load 读取配置文件（不存在则用默认配置），再叠加环境变量
func Load(path string) (*Config, error) {
	c := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err == nil {
			if err := yaml.Unmarshal(b, c); err != nil {
				return nil, fmt.Errorf("解析配置失败: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	c.applyEnv()
	c.EnsureDirs()
	c.ApplyTier(DetectTier()) // 显式配置优先，仅补齐未设置的资源项
	return c, nil
}

// 环境变量覆盖：NASPIC_DB_DRIVER / NASPIC_DB_DSN / NASPIC_PORT / NASPIC_DATA_DIR ...
func (c *Config) applyEnv() {
	set := func(env string, fn func(string)) {
		if v := os.Getenv(env); v != "" {
			fn(v)
		}
	}
	set("NASPIC_DB_DRIVER", func(v string) { c.Database.Driver = v })
	set("NASPIC_DB_DSN", func(v string) { c.Database.DSN = v })
	set("NASPIC_DATA_DIR", func(v string) { c.Server.DataDir = v })
	set("NASPIC_WEB_ROOT", func(v string) { c.Server.WebRoot = v })
	set("NASPIC_TIMEZONE", func(v string) { c.Server.TimeZone = v })
	set("NASPIC_MANAGED_ROOT", func(v string) { c.Storage.ManagedRoot = v })
	set("NASPIC_CACHE_ROOT", func(v string) { c.Storage.CacheRoot = v })
	set("NASPIC_LOG_LEVEL", func(v string) { c.Log.Level = v })
	set("NASPIC_REDIS_ADDR", func(v string) {
		c.Cache.Addr = v
		if os.Getenv("NASPIC_REDIS_ENABLED") == "" {
			c.Cache.Enabled = true // 给了地址就默认启用
		}
	})
	set("NASPIC_REDIS_PASSWORD", func(v string) { c.Cache.Password = v })
	set("NASPIC_REDIS_PREFIX", func(v string) { c.Cache.Prefix = v })
	if v := os.Getenv("NASPIC_REDIS_ENABLED"); v != "" {
		c.Cache.Enabled = strings.EqualFold(v, "1") || strings.EqualFold(v, "true") || strings.EqualFold(v, "on")
	}
	if v := os.Getenv("NASPIC_REDIS_DB"); v != "" {
		if d, err := strconv.Atoi(v); err == nil {
			c.Cache.DB = d
		}
	}
	if v := os.Getenv("NASPIC_REDIS_TTL"); v != "" {
		if d, err := strconv.Atoi(v); err == nil {
			c.Cache.TTLSec = d
		}
	}
	if v := os.Getenv("NASPIC_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Server.Port = p
		}
	}
	if v := os.Getenv("NASPIC_AI"); v != "" {
		c.AI.Enabled = strings.EqualFold(v, "1") || strings.EqualFold(v, "true") || strings.EqualFold(v, "on")
	}
}

// EnsureDirs 保证关键目录存在
func (c *Config) EnsureDirs() {
	dirs := []string{
		c.Server.DataDir,
		c.Storage.ManagedRoot,
		c.Storage.CacheRoot,
		filepath.Join(c.Storage.CacheRoot, "thumbs"),
		c.Storage.TempRoot,
	}
	if c.AI.Enabled {
		dirs = append(dirs, c.AI.ModelDir)
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		_ = os.MkdirAll(d, 0o755)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
