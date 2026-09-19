package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/naspic/naspic/internal/cache"
	"github.com/naspic/naspic/internal/model"
	"github.com/naspic/naspic/internal/storage"
)

// listMedia 时间轴分页：游标分页（taken_at, id），避免深翻页 OFFSET 性能崩塌。
// 支持筛选：相册库、媒体类型、收藏、关键词、日期区间。
func (s *Server) listMedia(c *gin.Context) {
	libID, _ := strconv.ParseInt(c.Query("library_id"), 10, 64)
	mt, _ := strconv.Atoi(c.Query("media_type")) // 0=全部 1=图片 2=视频 3=其他
	fav, _ := strconv.Atoi(c.Query("favorite"))  // 0=不限 1=仅收藏 2=仅未收藏（旧字段，UI 已改用相册）
	albumID, _ := strconv.ParseInt(c.Query("album_id"), 10, 64)
	kw := strings.TrimSpace(c.Query("keyword"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 300 {
		limit = 80
	}
	asc := c.Query("order") == "asc"

	q := s.db.Where("deleted_at IS NULL")
	if libID > 0 {
		q = q.Where("library_id = ?", libID)
	}
	if mt > 0 {
		q = q.Where("media_type = ?", mt)
	}
	if fav == 1 {
		q = q.Where("favorite = 1")
	} else if fav == 2 {
		q = q.Where("favorite IS NULL OR favorite = 0")
	}
	if albumID > 0 {
		// 只看某个相册里的照片
		q = q.Where("id IN (SELECT media_id FROM album_items WHERE album_id = ?)", albumID)
	}
	if kw != "" {
		like := "%" + strings.ToLower(kw) + "%"
		q = q.Where("(LOWER(filename) LIKE ? OR LOWER(relative_path) LIKE ?)", like, like)
	}
	if t := parseTimeParam(c.Query("start")); t != nil {
		q = q.Where("taken_at >= ?", *t)
	}
	if t := parseTimeParam(c.Query("end")); t != nil {
		// 结束日期按当天 23:59:59 处理
		q = q.Where("taken_at <= ?", t.Add(24*time.Hour-time.Second))
	}

	// 游标：要求 taken_at 非空，避免 NULL 参与比较导致翻页错乱
	cursorTime := parseTimeParam(c.Query("cursor_time"))
	cursorID, _ := strconv.ParseInt(c.Query("cursor_id"), 10, 64)

	// 缓存只覆盖首页（无游标、默认倒序）：翻页参数组合太多，命中率低还容易脏
	cacheable := s.cache != nil && s.cache.Available() && cursorTime == nil && !asc
	if cacheable {
		var cached gin.H
		if err := s.cache.GetJSON(cache.PfxMedia+"list:"+c.Request.URL.RawQuery, &cached); err == nil {
			c.Header("X-Cache", "HIT")
			ok(c, cached)
			return
		}
		c.Header("X-Cache", "MISS")
	}

	if cursorTime != nil && cursorID > 0 {
		q = q.Where("taken_at IS NOT NULL")
		if asc {
			q = q.Where("(taken_at > ? OR (taken_at = ? AND id > ?))", *cursorTime, *cursorTime, cursorID)
		} else {
			q = q.Where("(taken_at < ? OR (taken_at = ? AND id < ?))", *cursorTime, *cursorTime, cursorID)
		}
	}

	order := "taken_at DESC, id DESC"
	if asc {
		order = "taken_at ASC, id ASC"
	}

	// 多取一条用于判断是否还有下一页
	var list []model.MediaFile
	if err := q.Order(order).Limit(limit + 1).Find(&list).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	hasMore := false
	if len(list) > limit {
		hasMore = true
		list = list[:limit]
	}
	next := gin.H{}
	if hasMore && len(list) > 0 && list[len(list)-1].TakenAt != nil {
		last := list[len(list)-1]
		next = gin.H{"cursor_time": last.TakenAt.UTC().Format(time.RFC3339Nano), "cursor_id": last.ID}
	}
	body := gin.H{"items": list, "limit": limit, "has_more": hasMore, "next_cursor": next}
	if cacheable {
		s.cache.SetJSON(cache.PfxMedia+"list:"+c.Request.URL.RawQuery, body)
	}
	ok(c, body)
}

// getMediaFile 直读原图。挂载模式下直接从宿主机目录流式读取，不复制不修改
func (s *Server) getMediaFile(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var m model.MediaFile
	if err := s.db.First(&m, id).Error; err != nil {
		fail(c, 404, "媒体不存在")
		return
	}
	d, exist := s.drivers.Get(m.LibraryID)
	if !exist {
		fail(c, 500, "驱动未注册")
		return
	}
	f, err := d.GetFile(c, m.RelativePath)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			fail(c, 404, "源文件已不存在（挂载目录可能已移除）")
			return
		}
		fail(c, 500, err.Error())
		return
	}
	defer f.Close()

	ctype := m.Mime
	if ctype == "" {
		ctype = mimeByExt(m.Ext) // 历史数据 Mime 可能为空，按扩展名兜底
	}
	c.Header("Content-Type", ctype)
	if c.Query("download") == "1" {
		c.Header("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(m.Filename, `"`, "")+`"`)
	}
	c.Header("Cache-Control", "private, max-age=86400")
	c.DataFromReader(200, m.SizeBytes, ctype, f, nil)
}

// getMediaThumb 缩略图（懒加载：未生成时由 thumb 模块现场生成）
func (s *Server) getMediaThumb(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	size := storage.ThumbSize(c.DefaultQuery("size", string(storage.ThumbMD)))

	var m model.MediaFile
	if err := s.db.First(&m, id).Error; err != nil {
		fail(c, 404, "媒体不存在")
		return
	}
	d, exist := s.drivers.Get(m.LibraryID)
	if !exist {
		fail(c, 500, "驱动未注册")
		return
	}
	data, contentType, err := d.GetThumb(c, m.RelativePath, size)
	if err != nil {
		fail(c, 500, "缩略图不可用: "+err.Error())
		return
	}
	c.Header("Cache-Control", "private, max-age=604800")
	c.Data(200, contentType, data)
}

// getMediaDetail 媒体详情：基础信息 + EXIF + 所属库 + 标签
func (s *Server) getMediaDetail(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var m model.MediaFile
	if err := s.db.First(&m, id).Error; err != nil {
		fail(c, 404, "媒体不存在")
		return
	}
	var lib model.Library
	libName, rootPath := "", ""
	if err := s.db.Select("id,name,storage_root,type").First(&lib, m.LibraryID).Error; err == nil {
		libName = lib.Name
		rootPath = lib.StorageRoot
	}
	exif := map[string]interface{}{}
	if m.ExifJSON != "" {
		_ = json.Unmarshal([]byte(m.ExifJSON), &exif)
	}
	var tagIDs []int64
	s.db.Model(&model.MediaTag{}).Where("media_id = ?", id).Pluck("tag_id", &tagIDs)

	ok(c, gin.H{
		"media":        m,
		"library_name": libName,
		"library_root": rootPath,
		"exif":         exif,
		"tag_ids":      tagIDs,
	})
}

type updateMediaReq struct {
	Favorite *int8   `json:"favorite"` // 0/1
	TakenAt  *string `json:"taken_at"` // RFC3339，用于手工修正时间
}

// updateMedia 修改收藏标记 / 修正拍摄时间（挂载库只读，仅改索引不改原图）
func (s *Server) updateMedia(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req updateMediaReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	upd := map[string]interface{}{"updated_at": time.Now()}
	if req.Favorite != nil {
		upd["favorite"] = *req.Favorite
	}
	if req.TakenAt != nil && *req.TakenAt != "" {
		t, err := time.Parse(time.RFC3339, *req.TakenAt)
		if err != nil {
			fail(c, http.StatusBadRequest, "taken_at 格式应为 RFC3339")
			return
		}
		upd["taken_at"] = t.UTC()
		upd["taken_at_source"] = 3 // 3=手工修正
	}
	if len(upd) <= 1 {
		fail(c, http.StatusBadRequest, "没有可更新字段")
		return
	}
	if err := s.db.Model(&model.MediaFile{}).Where("id = ?", id).Updates(upd).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	s.invalidate(cache.PfxMedia)
	s.invalidate(cache.PfxDetail)
	ok(c, gin.H{"id": id, "updated": true})
}

type batchMediaReq struct {
	IDs      []int64 `json:"ids"`
	Action   string  `json:"action"` // favorite / unfavorite / delete
	Physical bool    `json:"physical"`
}

// batchMedia 批量收藏 / 取消收藏 / 删除（删除对挂载库只删索引，绝不删宿主机原图）
func (s *Server) batchMedia(c *gin.Context) {
	var req batchMediaReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if len(req.IDs) == 0 {
		fail(c, http.StatusBadRequest, "ids 不能为空")
		return
	}
	if len(req.IDs) > 2000 {
		fail(c, http.StatusBadRequest, "单次最多处理 2000 条")
		return
	}
	now := time.Now()
	switch req.Action {
	case "favorite", "unfavorite":
		v := int8(1)
		if req.Action == "unfavorite" {
			v = 0
		}
		res := s.db.Model(&model.MediaFile{}).
			Where("id IN ? AND deleted_at IS NULL", req.IDs).
			Updates(map[string]interface{}{"favorite": v, "updated_at": now})
		if res.Error != nil {
			fail(c, 500, res.Error.Error())
			return
		}
		s.invalidate(cache.PfxMedia)
		s.invalidate(cache.PfxDetail)
		ok(c, gin.H{"affected": res.RowsAffected, "favorite": v})
		return
	case "delete":
		var list []model.MediaFile
		s.db.Select("id,library_id,source_type,relative_path").
			Where("id IN ? AND deleted_at IS NULL", req.IDs).Find(&list)
		physical := 0
		if req.Physical {
			for _, m := range list {
				if m.SourceType == model.SourceMounted {
					continue
				}
				d, exist := s.drivers.Get(m.LibraryID)
				if !exist || !d.Writable() {
					continue
				}
				if err := d.Delete(c, m.RelativePath, true); err == nil {
					physical++
				}
			}
		}
		res := s.db.Model(&model.MediaFile{}).
			Where("id IN ? AND deleted_at IS NULL", req.IDs).
			Update("deleted_at", now)
		if res.Error != nil {
			fail(c, 500, res.Error.Error())
			return
		}
		s.invalidate(cache.PfxMedia)
		s.invalidate(cache.PfxDetail)
		s.invalidate(cache.PfxAlbum)
		ok(c, gin.H{"affected": res.RowsAffected, "index_deleted": true, "physical_deleted": physical})
		return
	default:
		fail(c, http.StatusBadRequest, "不支持的操作: "+req.Action)
	}
}

// mediaStats 概览统计：总数、图片/视频数、收藏数、占用空间、库列表
func (s *Server) mediaStats(c *gin.Context) {
	libID, _ := strconv.ParseInt(c.Query("library_id"), 10, 64)

	// 统计是全表 COUNT/SUM，翻页浏览时每个用户都要打一次，缓存收益最大
	if s.cache != nil && s.cache.Available() {
		var cached gin.H
		if err := s.cache.GetJSON(cache.PfxMedia+"stats:"+c.Request.URL.RawQuery, &cached); err == nil {
			c.Header("X-Cache", "HIT")
			ok(c, cached)
			return
		}
		c.Header("X-Cache", "MISS")
	}

	// 每次重新构造查询，避免复用链式 Statement 造成条件叠加
	q := func() *gorm.DB {
		x := s.db.Model(&model.MediaFile{}).Where("deleted_at IS NULL")
		if libID > 0 {
			x = x.Where("library_id = ?", libID)
		}
		return x
	}
	var total, images, videos, favs, bytes int64
	q().Count(&total)
	q().Where("media_type = ?", model.MediaImage).Count(&images)
	q().Where("media_type = ?", model.MediaVideo).Count(&videos)
	q().Where("favorite = 1").Count(&favs)
	if row := q().Select("COALESCE(SUM(size_bytes), 0)").Row(); row != nil {
		_ = row.Scan(&bytes)
	}

	type libItem struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Type       int8   `json:"type"`
		MediaCount int    `json:"media_count"`
	}
	var libs []libItem
	lq := s.db.Model(&model.Library{}).Select("id,name,type,media_count").Where("deleted_at IS NULL")
	if libID > 0 {
		lq = lq.Where("id = ?", libID)
	}
	_ = lq.Order("id ASC").Scan(&libs)

	body := gin.H{
		"total": total, "images": images, "videos": videos,
		"favorites": favs, "bytes": bytes, "libraries": libs,
	}
	if s.cache != nil && s.cache.Available() {
		s.cache.SetJSON(cache.PfxMedia+"stats:"+c.Request.URL.RawQuery, body)
	}
	ok(c, body)
}

// deleteMedia 删除媒体。
//   - 托管库：默认只删索引（回收站语义），physical=true 才删物理文件
//   - 挂载库：任何情况都不删宿主机原图，只删索引
func (s *Server) deleteMedia(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	physical := c.Query("physical") == "true"

	var m model.MediaFile
	if err := s.db.First(&m, id).Error; err != nil {
		fail(c, 404, "媒体不存在")
		return
	}
	d, exist := s.drivers.Get(m.LibraryID)
	if !exist {
		fail(c, 500, "驱动未注册")
		return
	}

	if m.SourceType == model.SourceMounted || !d.Writable() {
		// 挂载索引：只删索引记录，宿主机原图原封不动
		now := time.Now()
		if err := s.db.Model(&model.MediaFile{}).Where("id = ?", id).Update("deleted_at", now).Error; err != nil {
			fail(c, 500, err.Error())
			return
		}
		ok(c, gin.H{"id": id, "index_deleted": true, "physical_deleted": false})
		return
	}

	if physical {
		if err := d.Delete(c, m.RelativePath, true); err != nil {
			if errors.Is(err, storage.ErrReadOnly) {
				fail(c, 403, "只读模式，禁止删除")
				return
			}
			fail(c, 500, err.Error())
			return
		}
	}
	now := time.Now()
	s.db.Model(&model.MediaFile{}).Where("id = ?", id).Update("deleted_at", now)
	s.invalidate(cache.PfxMedia)
	s.invalidate(cache.PfxDetail)
	s.invalidate(cache.PfxAlbum)
	ok(c, gin.H{"id": id, "index_deleted": true, "physical_deleted": physical})
}

// parseTimeParam 兼容 RFC3339 与 2006-01-02 两种输入
func parseTimeParam(v string) *time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return &t
	}
	if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
		return &t
	}
	return nil
}
