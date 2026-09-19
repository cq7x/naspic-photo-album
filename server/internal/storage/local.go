package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
)

// LocalDriver 平台托管存储驱动：原始文件由平台保存，可读写。
// 目录结构：<root>/<library>/YYYY/MM/<filename>
type LocalDriver struct {
	libID    int64  // 所属相册库
	root     string // 该库的物理根目录
	hashFull int64  // 小于该体积走全量哈希
}

// NewLocalDriver 创建托管存储驱动
func NewLocalDriver(libID int64, root string, hashFullMaxBytes int64) (*LocalDriver, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("创建托管目录失败 %s: %w", abs, err)
	}
	if hashFullMaxBytes <= 0 {
		hashFullMaxBytes = 20 << 20
	}
	return &LocalDriver{libID: libID, root: abs, hashFull: hashFullMaxBytes}, nil
}

func (d *LocalDriver) Kind() DriverKind { return KindManaged }
func (d *LocalDriver) LibraryID() int64 { return d.libID }
func (d *LocalDriver) Writable() bool   { return true }
func (d *LocalDriver) Root() string     { return d.root }

func (d *LocalDriver) List(ctx context.Context, opt ListOption) ([]Entry, error) {
	dir := d.root
	if opt.Dir != "" {
		p, err := SafeJoin(d.root, opt.Dir)
		if err != nil {
			return nil, err
		}
		dir = p
	}
	entries := make([]Entry, 0, 64)
	err := filepath.WalkDir(dir, func(p string, de os.DirEntry, err error) error {
		if err != nil {
			return nil // 单文件异常不中断
		}
		if p == dir {
			return nil
		}
		if de.IsDir() {
			if !opt.Recursive {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(d.root, p)
		info, _ := de.Info()
		ext := ExtOf(p)
		entries = append(entries, Entry{
			Key:       filepath.ToSlash(rel),
			AbsPath:   p,
			Size:      infoOrZero(info),
			ModTime:   modTimeOrZero(info),
			Ext:       ext,
			MediaType: MediaTypeOf(ext),
		})
		return nil
	})
	return entries, err
}

func (d *LocalDriver) GetFile(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := SafeJoin(d.root, key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

func (d *LocalDriver) GetThumb(ctx context.Context, key string, size ThumbSize) ([]byte, string, error) {
	return ThumbOf(ctx, d, key, size)
}

func (d *LocalDriver) GetFileStat(ctx context.Context, key string) (*FileStat, error) {
	p, err := SafeJoin(d.root, key)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &FileStat{Key: key, Exists: false}, nil
		}
		return nil, err
	}
	hash, algo, _ := QuickHash(p, d.hashFull)
	mime := ""
	if mt, err := mimetype.DetectFile(p); err == nil && mt != nil {
		mime = mt.String()
	}
	return &FileStat{
		Key: key, AbsPath: p, Size: st.Size(), ModTime: st.ModTime(),
		IsDir: st.IsDir(), Mime: mime, Exists: true, Hash: hash, HashAlgo: algo,
	}, nil
}

// Scan 扫描托管目录并产出事件流。增量模式下只上报 mtime 晚于 Since 的条目。
func (d *LocalDriver) Scan(ctx context.Context, opt ScanOption) (<-chan ScanEvent, error) {
	out := make(chan ScanEvent, 64)
	go func() {
		defer close(out)
		_ = filepath.WalkDir(d.root, func(p string, de os.DirEntry, err error) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if err != nil {
				emit(out, ScanEvent{Type: EventError, Msg: err.Error()})
				return nil // 不中断整个任务
			}
			if de.IsDir() {
				rel, _ := filepath.Rel(d.root, p)
				if rel != "." && !opt.Recursive {
					return filepath.SkipDir
				}
				if MatchIgnore(rel, opt.IgnoreRules) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(d.root, p)
			relSlash := filepath.ToSlash(rel)
			if MatchIgnore(relSlash, opt.IgnoreRules) {
				return nil
			}
			ext := ExtOf(p)
			if !IsSupportedExt(ext) || !MatchInclude(ext, opt.IncludeExts) {
				return nil
			}
			info, ierr := de.Info()
			if ierr != nil {
				emit(out, ScanEvent{Type: EventError, Entry: Entry{Key: relSlash}, Msg: ierr.Error()})
				return nil
			}
			if !opt.Full && !opt.Since.IsZero() && info.ModTime().Before(opt.Since) {
				emit(out, ScanEvent{Type: EventSkipped, Entry: Entry{
					Key: relSlash, AbsPath: p, Size: info.Size(), ModTime: info.ModTime(),
					Ext: ext, MediaType: MediaTypeOf(ext),
				}})
				return nil
			}
			hash, algo, herr := QuickHash(p, d.hashFull)
			if herr != nil {
				emit(out, ScanEvent{Type: EventError, Entry: Entry{Key: relSlash}, Msg: herr.Error()})
				return nil
			}
			_ = algo
			emit(out, ScanEvent{Type: EventCreated, Hash: hash, Entry: Entry{
				Key: relSlash, AbsPath: p, Size: info.Size(), ModTime: info.ModTime(),
				Ext: ext, MediaType: MediaTypeOf(ext),
			}})
			return nil
		})
	}()
	return out, nil
}

// UploadFile 写入托管存储。
// 冲突策略：同名且 Overwrite=false 时自动重命名为 name (1).ext，**绝不覆盖**。
func (d *LocalDriver) UploadFile(ctx context.Context, r io.Reader, opt UploadOption) (*UploadResult, error) {
	if opt.RelPath == "" {
		return nil, errors.New("upload: rel_path 不能为空")
	}
	target, err := SafeJoin(d.root, opt.RelPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, err
	}

	renamed := false
	if _, err := os.Stat(target); err == nil && !opt.Overwrite {
		target = uniqueName(target)
		renamed = true
	}

	f, err := os.Create(target)
	if err != nil {
		return nil, err
	}
	h := &hashWriter{w: f}
	if _, err := io.Copy(h, r); err != nil {
		f.Close()
		os.Remove(target)
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if opt.Mtime.IsZero() {
		opt.Mtime = time.Now()
	}
	_ = os.Chtimes(target, opt.Mtime, opt.Mtime)

	rel, _ := filepath.Rel(d.root, target)
	return &UploadResult{
		RelPath: filepath.ToSlash(rel),
		AbsPath: target,
		Hash:    h.hex(),
		Size:    h.n,
		Renamed: renamed,
	}, nil
}

func (d *LocalDriver) Delete(ctx context.Context, key string, deletePhysical bool) error {
	if !deletePhysical {
		return nil // 托管库也支持仅删索引（回收站语义）
	}
	p, err := SafeJoin(d.root, key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ---------- 内部工具 ----------

func uniqueName(path string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	for i := 1; i < 10000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", name, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", name, time.Now().Unix(), ext))
}

func emit(ch chan<- ScanEvent, ev ScanEvent) {
	select {
	case ch <- ev:
	default:
		// 消费者过慢时丢弃 skipped 事件，保护内存
		if ev.Type != EventSkipped {
			ch <- ev
		}
	}
}

func infoOrZero(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
}

func modTimeOrZero(info os.FileInfo) time.Time {
	if info == nil {
		return time.Time{}
	}
	return info.ModTime()
}
