package storage

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// 监听模式。与 model 包取值保持一致，此处本地定义，
// 避免 storage 反向依赖 model（storage 是最底层能力包）。
const (
	watchInotify = 1
	watchPoll    = 2
)

// MountConfig 挂载目录驱动配置
type MountConfig struct {
	LibraryID    int64    // 所属相册库
	HostPath     string   // 宿主机绝对路径（容器内看到的挂载点）
	Mode         int8     // 1=只读（默认） 2=读写（高危）
	AllowDelete  bool     // 仅在读写模式下有意义
	WatchMode    int8     // 1=inotify 2=轮询
	PollInterval int      // 秒
	Recursive    bool
	IgnoreRules  []string
	IncludeExts  []string
	HashFullMax  int64
}

// MountLocalDirDriver 挂载宿主机已有照片目录的驱动。
// 核心约束：
//  1. 只读模式下不复制、不修改、不删除宿主机原图，只扫描建立索引；
//  2. 删除操作默认只删索引（deletePhysical=false），真实删除需读写模式 + AllowDelete 双开关；
//  3. inotify 不可用时自动降级为定时轮询（NFS/CIFS/overlay 常见）。
type MountLocalDirDriver struct {
	cfg      MountConfig
	root     string
	degraded bool      // 是否已从 inotify 降级为轮询
	mu       sync.RWMutex
}

// NewMountDriver 创建挂载目录驱动
func NewMountDriver(cfg MountConfig) (*MountLocalDirDriver, error) {
	if cfg.HostPath == "" {
		return nil, ErrNotFound
	}
	abs, err := filepath.Abs(cfg.HostPath)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, ErrNotFound
	}
	if cfg.Mode == 0 {
		cfg.Mode = 1 // 只读优先
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 300
	}
	if cfg.HashFullMax <= 0 {
		cfg.HashFullMax = 20 << 20
	}
	return &MountLocalDirDriver{cfg: cfg, root: abs}, nil
}

func (d *MountLocalDirDriver) Kind() DriverKind { return KindMounted }
func (d *MountLocalDirDriver) LibraryID() int64 { return d.cfg.LibraryID }

// Writable 只读模式恒 false，这是保护宿主机原图的第一道闸门
func (d *MountLocalDirDriver) Writable() bool { return d.cfg.Mode == 2 }
func (d *MountLocalDirDriver) Root() string   { return d.root }

// Degraded 是否已降级为轮询（供 Web 端展示告警）
func (d *MountLocalDirDriver) Degraded() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.degraded
}

func (d *MountLocalDirDriver) List(ctx context.Context, opt ListOption) ([]Entry, error) {
	dir := d.root
	if opt.Dir != "" {
		p, err := SafeJoin(d.root, opt.Dir)
		if err != nil {
			return nil, err
		}
		dir = p
	}
	entries := make([]Entry, 0, 64)
	_ = filepath.WalkDir(dir, func(p string, de os.DirEntry, err error) error {
		if err != nil || p == dir {
			return nil
		}
		if de.IsDir() {
			if !d.cfg.Recursive {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(d.root, p)
		relSlash := filepath.ToSlash(rel)
		if MatchIgnore(relSlash, d.cfg.IgnoreRules) {
			return nil
		}
		ext := ExtOf(p)
		info, _ := de.Info()
		entries = append(entries, Entry{
			Key: relSlash, AbsPath: p, Size: infoOrZero(info), ModTime: modTimeOrZero(info),
			Ext: ext, MediaType: MediaTypeOf(ext),
		})
		return nil
	})
	return entries, nil
}

func (d *MountLocalDirDriver) GetFile(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := SafeJoin(d.root, key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p) // 只读打开
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

func (d *MountLocalDirDriver) GetThumb(ctx context.Context, key string, size ThumbSize) ([]byte, string, error) {
	return ThumbOf(ctx, d, key, size)
}

func (d *MountLocalDirDriver) GetFileStat(ctx context.Context, key string) (*FileStat, error) {
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
	hash, algo, _ := QuickHash(p, d.cfg.HashFullMax)
	return &FileStat{
		Key: key, AbsPath: p, Size: st.Size(), ModTime: st.ModTime(),
		IsDir: st.IsDir(), Exists: true, Hash: hash, HashAlgo: algo,
	}, nil
}

func (d *MountLocalDirDriver) Scan(ctx context.Context, opt ScanOption) (<-chan ScanEvent, error) {
	rules := opt.IgnoreRules
	if len(rules) == 0 {
		rules = d.cfg.IgnoreRules
	}
	inc := opt.IncludeExts
	if len(inc) == 0 {
		inc = d.cfg.IncludeExts
	}
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
				return nil
			}
			if de.IsDir() {
				rel, _ := filepath.Rel(d.root, p)
				if rel != "." && !d.cfg.Recursive {
					return filepath.SkipDir
				}
				if MatchIgnore(rel, rules) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(d.root, p)
			relSlash := filepath.ToSlash(rel)
			if MatchIgnore(relSlash, rules) {
				return nil
			}
			ext := ExtOf(p)
			if !IsSupportedExt(ext) || !MatchInclude(ext, inc) {
				return nil
			}
			info, ierr := de.Info()
			if ierr != nil {
				emit(out, ScanEvent{Type: EventError, Entry: Entry{Key: relSlash}, Msg: ierr.Error()})
				return nil
			}
			if !opt.Full && !opt.Since.IsZero() && info.ModTime().Before(opt.Since) {
				emit(out, ScanEvent{Type: EventSkipped, Entry: Entry{
					Key: relSlash, AbsPath: p, Size: info.Size(), ModTime: info.ModTime(), Ext: ext}})
				return nil
			}
			hash, _, herr := QuickHash(p, d.cfg.HashFullMax)
			if herr != nil {
				emit(out, ScanEvent{Type: EventError, Entry: Entry{Key: relSlash}, Msg: herr.Error()})
				return nil
			}
			emit(out, ScanEvent{Type: EventCreated, Hash: hash, Entry: Entry{
				Key: relSlash, AbsPath: p, Size: info.Size(), ModTime: info.ModTime(),
				Ext: ext, MediaType: MediaTypeOf(ext),
			}})
			return nil
		})
	}()
	return out, nil
}

// UploadFile 挂载目录不接收上传，直接拒绝（保护原图）
func (d *MountLocalDirDriver) UploadFile(ctx context.Context, r io.Reader, opt UploadOption) (*UploadResult, error) {
	return nil, ErrReadOnly
}

// Delete 删除语义区分：
//   - deletePhysical=false：只删索引，由上层删除 media_files 记录（对只读目录唯一允许的方式）
//   - deletePhysical=true ：需要读写模式 + AllowDelete 双开关，否则返回 ErrReadOnly
func (d *MountLocalDirDriver) Delete(ctx context.Context, key string, deletePhysical bool) error {
	if !deletePhysical {
		return nil
	}
	if !d.Writable() || !d.cfg.AllowDelete {
		return ErrReadOnly
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

// Watch 文件变更检测：优先 inotify，失败自动降级为定时轮询。
// 返回的事件流只含「可能变化」的路径，上层再走增量入库。
func (d *MountLocalDirDriver) Watch(ctx context.Context) (<-chan ScanEvent, error) {
	out := make(chan ScanEvent, 32)

	if d.cfg.WatchMode == watchInotify {
		w, err := fsnotify.NewWatcher()
		if err == nil {
			if err := d.addWatchRecursive(w, d.root); err == nil {
				go d.watchLoop(ctx, w, out)
				return out, nil
			}
			_ = w.Close()
		}
		// 降级：标记并走轮询
		d.mu.Lock()
		d.degraded = true
		d.mu.Unlock()
	}
	go d.pollLoop(ctx, out)
	return out, nil
}

func (d *MountLocalDirDriver) watchLoop(ctx context.Context, w *fsnotify.Watcher, out chan<- ScanEvent) {
	defer close(out)
	defer w.Close()
	debounce := time.NewTicker(2 * time.Second)
	defer debounce.Stop()
	dirty := make(map[string]struct{})

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			// 新目录出现时动态加监听（inotify 非递归）
			if ev.Op&fsnotify.Create != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() && d.cfg.Recursive {
					_ = d.addWatchRecursive(w, ev.Name)
				}
			}
			rel, _ := filepath.Rel(d.root, ev.Name)
			dirty[filepath.ToSlash(rel)] = struct{}{}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			emit(out, ScanEvent{Type: EventError, Msg: err.Error()})
		case <-debounce.C:
			if len(dirty) > 0 {
				log.Printf("[watch] 库 %d inotify 捕获到 %d 个变更", d.cfg.LibraryID, len(dirty))
			}
			for rel := range dirty {
				ext := ExtOf(rel)
				if !IsSupportedExt(ext) {
					continue
				}
				st, serr := d.GetFileStat(ctx, rel)
				if serr != nil || !st.Exists {
					emit(out, ScanEvent{Type: EventMissing, Entry: Entry{Key: rel}})
					continue
				}
				emit(out, ScanEvent{Type: EventCreated, Hash: st.Hash, Entry: Entry{
					Key: rel, AbsPath: st.AbsPath, Size: st.Size, ModTime: st.ModTime,
					Ext: ext, MediaType: MediaTypeOf(ext),
				}})
			}
			dirty = make(map[string]struct{})
		}
	}
}

// pollLoop 轮询降级方案：按周期做一次增量扫描
func (d *MountLocalDirDriver) pollLoop(ctx context.Context, out chan<- ScanEvent) {
	defer close(out)
	interval := time.Duration(d.cfg.PollInterval) * time.Second
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	since := time.Now().Add(-interval * 2)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			ch, err := d.Scan(ctx, ScanOption{Recursive: d.cfg.Recursive, Since: since})
			if err != nil {
				emit(out, ScanEvent{Type: EventError, Msg: err.Error()})
				continue
			}
			for ev := range ch {
				if ev.Type == EventCreated || ev.Type == EventMissing {
					emit(out, ev)
				}
			}
			since = now.Add(-time.Second)
		}
	}
}

func (d *MountLocalDirDriver) addWatchRecursive(w *fsnotify.Watcher, dir string) error {
	if !d.cfg.Recursive {
		return w.Add(dir)
	}
	return filepath.WalkDir(dir, func(p string, de os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !de.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(d.root, p)
		if MatchIgnore(filepath.ToSlash(rel), d.cfg.IgnoreRules) {
			return filepath.SkipDir
		}
		return w.Add(p)
	})
}
