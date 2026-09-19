// Package storage 定义存储层抽象。
// 设计要点：
//  1. 所有驱动统一接口，托管存储（可写）与挂载目录（默认只读）行为差异由 Writable() 表达；
//  2. 挂载目录驱动只建立索引，绝不修改宿主机原图；
//  3. 扫描以 channel 事件流返回，便于上层限流与进度统计。
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"time"
)

// DriverKind 驱动类型
type DriverKind string

const (
	KindManaged DriverKind = "managed" // 平台托管存储
	KindMounted DriverKind = "mounted" // 挂载宿主机目录（只读优先）
)

// 常见错误
var (
	ErrReadOnly     = errors.New("storage: 只读模式，禁止修改宿主机原图")
	ErrNotFound     = errors.New("storage: 文件不存在")
	ErrNotSupported = errors.New("storage: 该驱动不支持此操作")
	ErrHashMismatch = errors.New("storage: 文件哈希校验失败")
)

// Entry 目录条目 / 扫描条目
type Entry struct {
	Key       string    `json:"key"`        // 相对库根路径
	AbsPath   string    `json:"abs_path"`   // 宿主机绝对路径（挂载模式有效）
	IsDir     bool      `json:"is_dir"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
	Ext       string    `json:"ext"`
	Mime      string    `json:"mime"`
	MediaType int8      `json:"media_type"` // 1=图片 2=视频 3=其他
}

// FileStat 文件状态
type FileStat struct {
	Key       string    `json:"key"`
	AbsPath   string    `json:"abs_path"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
	IsDir     bool      `json:"is_dir"`
	Mime      string    `json:"mime"`
	Exists    bool      `json:"exists"`
	Hash      string    `json:"hash"`
	HashAlgo  string    `json:"hash_algo"`
}

// ListOption 列举参数
type ListOption struct {
	Dir       string `json:"dir"`        // 相对目录，空=根
	Recursive bool   `json:"recursive"`
	Limit     int    `json:"limit"`
	Cursor    string `json:"cursor"`     // 上一页最后一个 Key
}

// ScanOption 扫描参数
type ScanOption struct {
	Full        bool     `json:"full"`         // true=全量重建索引
	Recursive   bool     `json:"recursive"`
	IgnoreRules []string `json:"ignore_rules"` // glob 规则
	IncludeExts []string `json:"include_exts"` // 为空表示全部支持格式
	Since       time.Time `json:"since"`       // 增量扫描起点（mtime 晚于此值）
	Batch       int      `json:"batch"`        // 每批数量
}

// ScanEventType 扫描事件类型
type ScanEventType string

const (
	EventCreated ScanEventType = "created" // 新增
	EventUpdated ScanEventType = "updated" // 变更
	EventDeleted ScanEventType = "deleted" // 源文件消失（仅索引层）
	EventSkipped ScanEventType = "skipped" // 无变化，跳过
	EventMissing ScanEventType = "missing" // 标记源文件缺失
	EventError   ScanEventType = "error"   // 解析/读取异常（不中断任务）
)

// ScanEvent 扫描事件
type ScanEvent struct {
	Type  ScanEventType `json:"type"`
	Entry Entry         `json:"entry"`
	Hash  string        `json:"hash"`
	Err   error         `json:"-"`
	Msg   string        `json:"msg"`
}

// UploadOption 上传参数
type UploadOption struct {
	RelPath   string    `json:"rel_path"`   // 相对库根的目标路径
	Size      int64     `json:"size"`
	Mime      string    `json:"mime"`
	Mtime     time.Time `json:"mtime"`
	Overwrite bool      `json:"overwrite"` // false 且同名存在时自动重命名，绝不覆盖
}

// UploadResult 上传结果
type UploadResult struct {
	RelPath string `json:"rel_path"`
	AbsPath string `json:"abs_path"`
	Hash    string `json:"hash"`
	Size    int64  `json:"size"`
	Dedup   bool   `json:"dedup"`  // 命中已有哈希，秒传
	Renamed bool   `json:"renamed"` // 同名冲突，服务端自动重命名
}

// ThumbSize 缩略图尺寸规格
type ThumbSize string

const (
	ThumbSM ThumbSize = "sm" // 256
	ThumbMD ThumbSize = "md" // 512
	ThumbLG ThumbSize = "lg" // 1080
	ThumbSQ ThumbSize = "sq" // 300 方图
)

// ThumbProvider 缩略图生成器（由 internal/thumb 实现并注册，见 v0.5.0）
type ThumbProvider interface {
	// Get 返回缩略图字节与内容类型；未就绪时返回 ErrThumbPending
	Get(ctx context.Context, drv StorageDriver, key string, size ThumbSize) ([]byte, string, error)
}

// ErrThumbPending 缩略图正在生成队列中
var ErrThumbPending = errors.New("storage: 缩略图生成中，请稍后重试")

var globalThumb ThumbProvider

// RegisterThumbProvider 注册缩略图生成器
func RegisterThumbProvider(p ThumbProvider) { globalThumb = p }

// ThumbOf 统一取缩略图入口
func ThumbOf(ctx context.Context, drv StorageDriver, key string, size ThumbSize) ([]byte, string, error) {
	if globalThumb == nil {
		return nil, "", ErrNotSupported
	}
	return globalThumb.Get(ctx, drv, key, size)
}

// StorageDriver 存储驱动统一接口
type StorageDriver interface {
	// Kind 驱动类型
	Kind() DriverKind
	// LibraryID 所属相册库 ID（缩略图缓存按库定位媒体记录）
	LibraryID() int64
	// Writable 是否允许写入/删除物理文件。挂载只读模式恒为 false
	Writable() bool
	// Root 返回物理根路径（挂载模式为宿主机路径，仅用于展示与诊断）
	Root() string

	List(ctx context.Context, opt ListOption) ([]Entry, error)
	GetFile(ctx context.Context, key string) (io.ReadCloser, error)
	GetThumb(ctx context.Context, key string, size ThumbSize) ([]byte, string, error)
	Scan(ctx context.Context, opt ScanOption) (<-chan ScanEvent, error)
	GetFileStat(ctx context.Context, key string) (*FileStat, error)
	UploadFile(ctx context.Context, r io.Reader, opt UploadOption) (*UploadResult, error)
	// Delete 删除：托管模式删物理文件；挂载模式只删除索引（且需 allow_delete）
	Delete(ctx context.Context, key string, deletePhysical bool) error
}

// ---------- 哈希工具 ----------

// HashFile 计算文件 SHA-256
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashReader 计算流 SHA-256
func HashReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// QuickHash 大文件快速哈希：头 1MB + 尾 1MB + 文件大小。
// 用于手机端/服务端对大文件的低成本增量识别，碰撞概率极低且速度快。
func QuickHash(path string, fullMaxBytes int64) (hash string, algo string, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	if st.Size() <= fullMaxBytes {
		h, err := HashFile(path)
		return h, "sha256", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	h := sha256.New()
	const seg = 1 << 20
	buf := make([]byte, seg)

	n, _ := io.ReadFull(f, buf)
	h.Write(buf[:n])
	if _, err := f.Seek(-seg, io.SeekEnd); err == nil {
		n, _ = io.ReadFull(f, buf)
		h.Write(buf[:n])
	}
	h.Write([]byte{byte(st.Size() >> 56), byte(st.Size() >> 48), byte(st.Size() >> 40),
		byte(st.Size() >> 32), byte(st.Size() >> 24), byte(st.Size() >> 16),
		byte(st.Size() >> 8), byte(st.Size())})
	return hex.EncodeToString(h.Sum(nil)), "fast", nil
}
