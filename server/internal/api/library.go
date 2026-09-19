package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/naspic/naspic/internal/cache"
	"github.com/naspic/naspic/internal/model"
	"github.com/naspic/naspic/internal/scanner"
	"github.com/naspic/naspic/internal/storage"
)

// listLibraries 相册库列表
func (s *Server) listLibraries(c *gin.Context) {
	var libs []model.Library
	if err := s.db.Where("deleted_at IS NULL").Order("id ASC").Find(&libs).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	// 类型要先声明：下面缓存读取要用到
	type item struct {
		model.Library
		DriverKind string `json:"driver_kind"`
		Writable   bool   `json:"writable"`
		Degraded   bool   `json:"degraded"`
	}

	if s.cache != nil && s.cache.Available() {
		var cached []item
		if err := s.cache.GetJSON(cache.PfxLibrary+"list", &cached); err == nil && cached != nil {
			c.Header("X-Cache", "HIT")
			ok(c, cached)
			return
		}
		c.Header("X-Cache", "MISS")
	}

	out := make([]item, 0, len(libs))
	for _, l := range libs {
		it := item{Library: l}
		if d, ok2 := s.drivers.Get(l.ID); ok2 {
			it.DriverKind = string(d.Kind())
			it.Writable = d.Writable()
			if md, isMount := d.(*storage.MountLocalDirDriver); isMount {
				it.Degraded = md.Degraded()
			}
		}
		out = append(out, it)
	}
	if s.cache != nil && s.cache.Available() {
		s.cache.SetJSON(cache.PfxLibrary+"list", out)
	}
	ok(c, out)
}

type createLibraryReq struct {
	Name        string `json:"name"`
	Type        int8   `json:"type"` // 1=托管 2=挂载
	StorageRoot string `json:"storage_root"`
	OwnerID     int64  `json:"owner_id"`
}

// createLibrary 创建相册库（托管类型）
func (s *Server) createLibrary(c *gin.Context) {
	var req createLibraryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if req.Name == "" {
		fail(c, http.StatusBadRequest, "库名不能为空")
		return
	}
	if req.Type == 0 {
		req.Type = model.LibraryTypeManaged
	}
	if req.Type == model.LibraryTypeMounted {
		fail(c, http.StatusBadRequest, "挂载库请使用 /api/v1/mount-dirs 创建")
		return
	}
	root := req.StorageRoot
	if root == "" {
		// 留空最安全：落在托管根（/data/managed）里，容器重建数据还在
		root = filepath.Join(s.cfg.Storage.ManagedRoot, slug(req.Name))
	} else if abs, err := filepath.Abs(root); err == nil {
		// 自定义路径必须先挂好卷并且目录已存在。
		// 否则 NewLocalDriver 会 MkdirAll 出一个容器内的空目录，
		// 文件写进可写层 —— 容器一重建，原图全没了（这个坑真趟过，整库原图丢失）。
		if st, serr := os.Stat(abs); serr != nil || !st.IsDir() {
			fail(c, http.StatusBadRequest, "目录不存在：请先在服务器上创建 "+abs+
				" 并把该卷挂进容器（compose 的 volumes）。不挂卷的话文件会存在容器里，重启即丢失。留空可自动使用安全目录。")
			return
		}
		root = abs
	}
	now := time.Now()
	lib := model.Library{
		Name: req.Name, Type: req.Type,
		OwnerID:     firstPositive(req.OwnerID, CurrentUserID(c)),
		StorageRoot: root, Status: 1,
		CreatedAt: &now, UpdatedAt: &now,
	}
	if err := s.db.Create(&lib).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	if err := s.registerLibraryDriver(lib); err != nil {
		fail(c, 500, "驱动注册失败: "+err.Error())
		return
	}
	s.invalidate(cache.PfxLibrary)
	s.invalidate(cache.PfxMedia)
	ok(c, lib)
}

type createMountDirReq struct {
	LibraryName    string   `json:"library_name"`
	HostPath       string   `json:"host_path"`
	Mode           int8     `json:"mode"`             // 1=只读（默认） 2=读写（高危）
	AllowDelete    bool     `json:"allow_delete"`     // 读写模式下的删除开关
	WatchMode      int8     `json:"watch_mode"`       // 1=inotify 2=轮询
	PollIntervalSec int     `json:"poll_interval_sec"`
	Recursive      *bool    `json:"recursive"`
	IgnoreRules    []string `json:"ignore_rules"`
	IncludeExts    []string `json:"include_exts"`
}

// createMountDir 添加挂载本地目录（只读优先）
func (s *Server) createMountDir(c *gin.Context) {
	var req createMountDirReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if req.HostPath == "" {
		fail(c, http.StatusBadRequest, "host_path 不能为空")
		return
	}

	// ---- 容器视角的路径预校验（必须在建库之前做，否则会留下僵尸记录）----
	// 最常见的坑：宿主机上目录确实存在，但容器启动时没有用 -v 把它挂进去，
	// 此时 stat 必然失败。这里给出可操作的提示，而不是抛 500。
	if !filepath.IsAbs(req.HostPath) {
		fail(c, http.StatusBadRequest, "请填写绝对路径，例如 /photos 或 /mnt/usb/travel")
		return
	}
	absPath := filepath.Clean(req.HostPath)
	st, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			fail(c, http.StatusBadRequest, "目录在容器内不可见："+absPath+
				"。请确认已用 docker -v 挂载进容器（例如 -v "+absPath+":"+absPath+":ro）并重启容器")
			return
		}
		fail(c, http.StatusBadRequest, "目录无法访问："+err.Error())
		return
	}
	if !st.IsDir() {
		fail(c, http.StatusBadRequest, "该路径不是文件夹，请填写目录路径")
		return
	}
	// 有 stat 权限不代表能列目录，实际读一次才算数
	if rerr := readableDir(absPath); rerr != nil {
		fail(c, http.StatusBadRequest, "目录不可读（容器内权限不足）："+rerr.Error())
		return
	}

	if req.Mode == 0 {
		req.Mode = model.MountModeReadOnly
	}
	if req.Mode == model.MountModeReadWrite && !req.AllowDelete {
		// 允许读写上传，但默认禁止删除宿主机原图
		req.AllowDelete = false
	}
	if req.WatchMode == 0 {
		req.WatchMode = model.WatchPoll
	}
	if req.PollIntervalSec <= 0 {
		req.PollIntervalSec = s.cfg.Scan.PollInterval
	}
	recursive := true
	if req.Recursive != nil {
		recursive = *req.Recursive
	}
	name := req.LibraryName
	if name == "" {
		name = filepath.Base(req.HostPath)
	}

	now := time.Now()
	lib := model.Library{
		Name: name, Type: model.LibraryTypeMounted,
		OwnerID: CurrentUserID(c), StorageRoot: absPath, Status: 1,
		CreatedAt: &now, UpdatedAt: &now,
	}
	if err := s.db.Create(&lib).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ignore, _ := json.Marshal(orEmpty(req.IgnoreRules))
	include, _ := json.Marshal(orEmpty(req.IncludeExts))
	md := model.MountDir{
		LibraryID: lib.ID, HostPath: absPath, Mode: req.Mode,
		AllowDelete: boolToInt8(req.AllowDelete), WatchMode: req.WatchMode,
		PollIntervalSec: req.PollIntervalSec, Recursive: boolToInt8(recursive),
		IgnoreRules: string(ignore), IncludeExts: string(include),
		ScanStatus: 1, CreatedAt: &now, UpdatedAt: &now,
	}
	if err := s.db.Create(&md).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	if err := s.registerMountDriver(lib, md); err != nil {
		// 回滚：驱动注册失败 = 这个库根本不可用。必须连同库记录一起清掉，
		// 否则会留下 driver_kind 为空的僵尸库——扫描必失败、界面上还删不掉。
		now := time.Now()
		s.db.Model(&model.Library{}).Where("id = ?", lib.ID).Update("deleted_at", now)
		s.db.Model(&model.MountDir{}).Where("library_id = ?", lib.ID).Update("deleted_at", now)
		fail(c, http.StatusBadRequest, "目录注册失败："+err.Error())
		return
	}

	// 监听目录变更。用 Background 而非请求的 ctx——gin 请求结束会 cancel，
	// 传请求 ctx 会让监听 goroutine 立刻退出。
	if s.watch != nil {
		s.watch.StartOne(context.Background(), md)
	}

	// 添加后立刻跑一次全量扫描。否则界面提示"已开始扫描"，
	// 实际要等到下一个轮询周期，用户会以为挂载失败。
	scanning := false
	if s.scanMgr != nil {
		if err := s.scanMgr.Submit(scanner.JobRequest{
			LibraryID: lib.ID, Full: true, TriggerSrc: 1,
		}); err != nil {
			if !errors.Is(err, scanner.ErrJobRunning) {
				log.Printf("[mount] 库 %d 首次扫描提交失败: %v", lib.ID, err)
			} else {
				scanning = true
			}
		} else {
			scanning = true
		}
	}
	s.invalidate(cache.PfxLibrary)
	s.invalidate(cache.PfxMedia)
	ok(c, gin.H{"library": lib, "mount_dir": md, "scanning": scanning})
}

// readableDir 真正列一次目录，确认可读（stat 通过不代表能读）
func readableDir(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}

// listMountDirs 挂载目录列表
func (s *Server) listMountDirs(c *gin.Context) {
	var dirs []model.MountDir
	if err := s.db.Where("deleted_at IS NULL").Find(&dirs).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, dirs)
}

// deleteLibrary 删除库。挂载库：仅删索引，绝不触碰宿主机原图
func (s *Server) deleteLibrary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	var lib model.Library
	if err := s.db.First(&lib, id).Error; err != nil {
		fail(c, 404, "库不存在")
		return
	}
	now := time.Now()
	s.db.Model(&model.MediaFile{}).Where("library_id = ?", id).Update("deleted_at", now)
	s.db.Model(&model.MountDir{}).Where("library_id = ?", id).Update("deleted_at", now)
	s.db.Model(&model.Library{}).Where("id = ?", id).Update("deleted_at", now)
	if s.watch != nil {
		s.watch.StopOne(id)
	}
	s.drivers.Remove(id)
	s.invalidate(cache.PfxLibrary)
	s.invalidate(cache.PfxMedia)
	ok(c, gin.H{"id": id})
}

// triggerScan 手动触发扫描
func (s *Server) triggerScan(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	var req struct {
		Full  bool `json:"full"`
		Force bool `json:"force"` // 忽略扫描缓存，强制重算（缓存疑似不准时用）
	}
	_ = c.ShouldBindJSON(&req)
	if err := s.scanMgr.Submit(scanner.JobRequest{LibraryID: id, Full: req.Full, Force: req.Force, TriggerSrc: 1}); err != nil {
		if errors.Is(err, scanner.ErrJobRunning) {
			fail(c, 409, "该库正在扫描中")
			return
		}
		fail(c, 500, err.Error())
		return
	}
	ok(c, gin.H{"submitted": true})
}

// libraryCache 库的缓存概况：缩略图缓存（可重建）+ 扫描指纹缓存（可重建）
func (s *Server) libraryCache(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	var rows int64
	s.db.Model(&model.ScanCache{}).Where("library_id = ?", id).Count(&rows)

	files, bytes := dirStat(filepath.Join(s.cfg.Storage.CacheRoot, "thumbs", strconv.FormatInt(id, 10)))

	var last model.ScanJob
	lastScan := gin.H{}
	if err := s.db.Where("library_id = ? AND status = 3", id).Order("id DESC").First(&last).Error; err == nil {
		lastScan = gin.H{
			"id": last.ID, "type": last.Type, "started_at": last.StartedAt,
			"scanned": last.Scanned, "added": last.Added, "skipped": last.Skipped,
			"missing": last.Missing, "failed": last.Failed,
		}
	}
	ok(c, gin.H{
		"library_id": id,
		"thumb":      gin.H{"files": files, "bytes": bytes},
		"scan_cache": gin.H{"rows": rows},
		"last_scan":  lastScan,
	})
}

// clearLibraryCache 清理缓存。scope: thumbs=仅缩略图 scan=仅扫描指纹 all=全部
// 二者都是纯粹的可重建缓存，清掉不会丢任何索引数据，只是下次扫描/浏览会更慢。
func (s *Server) clearLibraryCache(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	var req struct {
		Scope string `json:"scope"`
	}
	_ = c.ShouldBindJSON(&req)
	scope := req.Scope
	if scope == "" {
		scope = "all"
	}

	var removedFiles int64
	var removedBytes int64
	var removedRows int64

	if scope == "thumbs" || scope == "all" {
		dir := filepath.Join(s.cfg.Storage.CacheRoot, "thumbs", strconv.FormatInt(id, 10))
		files, bytes := dirStat(dir)
		if err := os.RemoveAll(dir); err != nil {
			fail(c, 500, "清理缩略图缓存失败: "+err.Error())
			return
		}
		removedFiles, removedBytes = files, bytes
	}
	if scope == "scan" || scope == "all" {
		res := s.db.Where("library_id = ?", id).Delete(&model.ScanCache{})
		if res.Error != nil {
			fail(c, 500, "清理扫描缓存失败: "+res.Error.Error())
			return
		}
		removedRows = res.RowsAffected
	}
	ok(c, gin.H{
		"scope": scope, "removed_files": removedFiles,
		"removed_bytes": removedBytes, "removed_rows": removedRows,
	})
}

// dirStat 统计目录内文件数与总字节数
func dirStat(dir string) (int64, int64) {
	var files, bytes int64
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		files++
		bytes += info.Size()
		return nil
	})
	return files, bytes
}

// listScanJobs 扫描任务列表
func (s *Server) listScanJobs(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var jobs []model.ScanJob
	q := s.db.Order("id DESC").Limit(20)
	if id > 0 {
		q = q.Where("library_id = ?", id)
	}
	if err := q.Find(&jobs).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, jobs)
}

// scanJobLogs 扫描日志
func (s *Server) scanJobLogs(c *gin.Context) {
	jobID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var logs []model.ScanLog
	if err := s.db.Where("job_id = ?", jobID).Order("id DESC").Limit(200).Find(&logs).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, logs)
}

// ---------- 驱动注册 ----------

func (s *Server) registerLibraryDriver(lib model.Library) error {
	d, err := storage.NewLocalDriver(lib.ID, lib.StorageRoot, int64(s.cfg.Sync.HashFullMaxBytes))
	if err != nil {
		return err
	}
	s.drivers.Register(lib.ID, d)
	return nil
}

func (s *Server) registerMountDriver(lib model.Library, md model.MountDir) error {
	var ignore, include []string
	_ = json.Unmarshal([]byte(md.IgnoreRules), &ignore)
	_ = json.Unmarshal([]byte(md.IncludeExts), &include)
	d, err := storage.NewMountDriver(storage.MountConfig{
		LibraryID: lib.ID, HostPath: md.HostPath, Mode: md.Mode, AllowDelete: md.AllowDelete == 1,
		WatchMode: md.WatchMode, PollInterval: md.PollIntervalSec,
		Recursive: md.Recursive == 1, IgnoreRules: ignore, IncludeExts: include,
		HashFullMax: int64(s.cfg.Sync.HashFullMaxBytes),
	})
	if err != nil {
		return err
	}
	s.drivers.Register(lib.ID, d)
	return nil
}

// ---------- 小工具 ----------

func slug(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || r == '-' || r == '_' {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "library"
	}
	return string(out)
}

func firstPositive(vals ...int64) int64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

func boolToInt8(b bool) int8 {
	if b {
		return 1
	}
	return 0
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
