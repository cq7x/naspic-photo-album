// Package cache 基于 Redis 的只读缓存层。
//
// 设计原则（很重要）：
//  1. **Redis 挂了不能拖垮相册**。所有读写都带「不可用即跳过」的降级：
//     连不上、超时、反序列化失败，一律当作未命中，直接回源查库。
//  2. 只缓存「读多写少、可容忍秒级延迟」的列表/统计数据，
//     不缓存媒体文件本身（体积大、走缩略图缓存更合适）。
//  3. 失效靠显式 Invalidate：上传、删除、扫描、相册变更时清掉对应前缀。
//     宁可多清，绝不能让用户看到改了但没变的数据。
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrDisabled 缓存不可用（未配置 / 连不上），调用方应直接回源
var ErrDisabled = errors.New("缓存不可用")

// Service 缓存服务
type Service struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// Config 缓存配置（由 config.Config 映射而来）
type Config struct {
	Enabled  bool
	Addr     string
	Password string
	DB       int
	Prefix   string
	TTLSec   int
}

// New 创建缓存服务。地址为空或连不上时返回 (nil, nil)，由调用方降级。
func New(cfg Config) (*Service, error) {
	if !cfg.Enabled || cfg.Addr == "" {
		return nil, nil
	}
	ttl := time.Duration(cfg.TTLSec) * time.Second
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "naspic:"
	}

	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
		MaxRetries:   1,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[cache] Redis 不可用（%s），本次启动不启用缓存: %v", cfg.Addr, err)
		_ = client.Close()
		return nil, nil
	}
	log.Printf("[cache] Redis 已连接 %s ttl=%v", cfg.Addr, ttl)
	return &Service{client: client, prefix: prefix, ttl: ttl}, nil
}

func (s *Service) Close() {
	if s != nil && s.client != nil {
		_ = s.client.Close()
	}
}

// Available 缓存是否可用
func (s *Service) Available() bool { return s != nil && s.client != nil }

func (s *Service) key(k string) string { return s.prefix + k }

// GetJSON 取缓存并反序列化到 dest，未命中返回 ErrDisabled 由调用方回源
func (s *Service) GetJSON(key string, dest interface{}) error {
	if !s.Available() {
		return ErrDisabled
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	b, err := s.client.Get(ctx, s.key(key)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ErrDisabled // 未命中，等价于不可用：回源
		}
		return ErrDisabled
	}
	if len(b) == 0 {
		return ErrDisabled
	}
	if err := json.Unmarshal(b, dest); err != nil {
		// 缓存内容坏了（版本升级导致结构变化）：丢掉它，别让错误扩散
		go s.Del(key)
		return ErrDisabled
	}
	return nil
}

// SetJSON 写缓存
func (s *Service) SetJSON(key string, v interface{}) {
	if !s.Available() {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.client.Set(ctx, s.key(key), b, s.ttl).Err()
}

// Del 删单个 key
func (s *Service) Del(keys ...string) {
	if !s.Available() || len(keys) == 0 {
		return
	}
	full := make([]string, 0, len(keys))
	for _, k := range keys {
		full = append(full, s.key(k))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.client.Del(ctx, full...).Err()
}

// DelPrefix 按前缀批量清理（用 SCAN，不用 KEYS——KEYS 会阻塞 Redis）
func (s *Service) DelPrefix(prefix string) {
	if !s.Available() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	match := s.key(prefix) + "*"
	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, match, 500).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			_ = s.client.Del(ctx, keys...).Err()
		}
		cursor = next
		if cursor == 0 {
			return
		}
	}
}

// ---------- 全局入口 ----------
// scanner / thumb 这些包拿不到 *api.Server，又需要在写库后主动失效缓存，
// 所以在进程内保留一个全局实例，由 main 启动时 SetGlobal。
var current *Service

// SetGlobal 登记全局缓存实例
func SetGlobal(s *Service) { current = s }

// Invalidate 按前缀清理（未启用缓存时为空操作，可放心调用）
func Invalidate(prefix string) {
	if current != nil {
		current.DelPrefix(prefix)
	}
}

// ---------- 缓存 key 前缀（失效时按前缀清） ----------

const (
	PfxMedia   = "media:"   // 媒体列表 / 统计
	PfxLibrary = "library:" // 相册库列表
	PfxAlbum   = "album:"   // 相册列表
	PfxDetail  = "detail:"  // 媒体详情
)
