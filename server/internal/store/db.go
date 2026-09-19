// Package store 数据库初始化与通用查询封装
package store

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	glogger "gorm.io/gorm/logger"

	"github.com/naspic/naspic/internal/config"
	"github.com/naspic/naspic/internal/model"
)

// Init 初始化数据库连接并执行自动迁移
func Init(cfg *config.Config) (*gorm.DB, error) {
	var (
		db  *gorm.DB
		err error
	)
	level := glogger.Warn
	switch strings.ToLower(cfg.Database.LogLevel) {
	case "silent":
		level = glogger.Silent
	case "error":
		level = glogger.Error
	case "info":
		level = glogger.Info
	}

	gcfg := &gorm.Config{
		Logger:                                   glogger.Default.LogMode(level),
		PrepareStmt:                              true,
		SkipDefaultTransaction:                   true,
		DisableForeignKeyConstraintWhenMigrating: true,
	}

	switch strings.ToLower(cfg.Database.Driver) {
	case "mysql":
		db, err = gorm.Open(mysql.Open(cfg.Database.DSN), gcfg)
	case "sqlite":
		fallthrough
	default:
		// 使用纯 Go 的 SQLite 实现（glebarez/sqlite），避免 cgo，
		// 交叉编译 arm64 时无需 C 交叉工具链。
		db, err = gorm.Open(sqlite.Open(cfg.Database.DSN), gcfg)
	}
	if err != nil {
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	maxOpen := cfg.Database.MaxOpen
	if maxOpen <= 0 {
		maxOpen = 10
	}
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdle)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if strings.ToLower(cfg.Database.Driver) != "mysql" {
		// SQLite 优化：WAL 提升并发读，busy_timeout 减少锁冲突
		db.Exec("PRAGMA journal_mode = WAL;")
		db.Exec("PRAGMA synchronous = NORMAL;")
		db.Exec("PRAGMA busy_timeout = 5000;")
		db.Exec("PRAGMA cache_size = -16000;")
	}

	if err := AutoMigrate(db); err != nil {
		return nil, err
	}
	log.Printf("[store] 数据库就绪 driver=%s", cfg.Database.Driver)
	return db, nil
}

// AutoMigrate 建表/补列
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.User{}, &model.Group{}, &model.UserGroup{},
		&model.Library{}, &model.MountDir{},
		&model.MediaFile{},
		&model.Tag{}, &model.MediaTag{},
		&model.Album{}, &model.AlbumItem{},
		&model.Thumbnail{},
		&model.Permission{},
		&model.SyncDevice{}, &model.SyncTask{}, &model.SyncRecord{},
		&model.UploadSession{},
		&model.ScanJob{}, &model.ScanLog{}, &model.ScanCache{},
		&model.Face{}, &model.FaceCluster{}, &model.MediaScene{},
		&model.SchemaMigration{},
	)
}
