package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naspic/naspic/internal/cache"
	"github.com/naspic/naspic/internal/model"
)

// 相册（v1.6.0 取代「收藏」）：
//   - 手动相册：往里加照片/移出照片
//   - 规则相册：按时间区间 / 关键词 / 相册库 一键匹配，命中即写入 album_items
//
// 规则只是「怎么把照片挑出来」的筛选条件，匹配完仍落成实体关联行，
// 所以之后手动增删都不会被规则覆盖掉。

// AlbumRule 相册规则（RuleJSON 的结构）
type AlbumRule struct {
	Start      string `json:"start,omitempty"`       // 2026-01-01
	End        string `json:"end,omitempty"`         // 2026-12-31（含当天）
	Keyword    string `json:"keyword,omitempty"`     // 文件名/路径包含
	LibraryID  int64  `json:"library_id,omitempty"`  // 限定某个相册库
	MediaType  int    `json:"media_type,omitempty"`  // 1=图片 2=视频
	MinFavLike bool   `json:"min_fav_like,omitempty"` // 兼容：老收藏数据
}

func parseRule(s string) AlbumRule {
	var r AlbumRule
	if strings.TrimSpace(s) != "" {
		_ = json.Unmarshal([]byte(s), &r)
	}
	return r
}

func (r AlbumRule) json() string {
	b, _ := json.Marshal(r)
	return string(b)
}

// listAlbums 相册列表（带条目数）
func (s *Server) listAlbums(c *gin.Context) {
	if s.cache != nil && s.cache.Available() {
		var cached gin.H
		if err := s.cache.GetJSON(cache.PfxAlbum+"list", &cached); err == nil {
			c.Header("X-Cache", "HIT")
			ok(c, cached)
			return
		}
		c.Header("X-Cache", "MISS")
	}
	var list []model.Album
	if err := s.db.Where("deleted_at IS NULL").
		Order("sort_order ASC, id ASC").Find(&list).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	type out struct {
		model.Album
		CoverID    int64  `json:"cover_id"`
		CoverThumb string `json:"cover_thumb"`
		RealCount  int64  `json:"real_count"`
	}
	items := make([]out, 0, len(list))
	for _, a := range list {
		o := out{Album: a}
		s.db.Model(&model.AlbumItem{}).Where("album_id = ?", a.ID).Count(&o.RealCount)
		// 封面：显式指定优先，否则取相册里最新加入的一张
		var m model.MediaFile
		q := s.db.Where("deleted_at IS NULL")
		if a.CoverPath != "" {
			q = q.Where("relative_path = ?", a.CoverPath)
		} else {
			q = q.Where("id IN (SELECT media_id FROM album_items WHERE album_id = ?)", a.ID)
		}
		if err := q.Order("taken_at DESC, id DESC").First(&m).Error; err == nil {
			o.CoverID = m.ID
			o.CoverThumb = "/api/v1/media/" + strconv.FormatInt(m.ID, 10) + "/thumb?size=md"
		}
		if o.RealCount != int64(a.ItemCount) {
			s.db.Model(&model.Album{}).Where("id = ?", a.ID).Update("item_count", o.RealCount)
		}
		items = append(items, o)
	}
	body := gin.H{"items": items}
	if s.cache != nil && s.cache.Available() {
		s.cache.SetJSON(cache.PfxAlbum+"list", body)
	}
	ok(c, body)
}

type albumReq struct {
	Name      string     `json:"name"`
	Remark    string     `json:"remark"`
	CoverPath string     `json:"cover_path"`
	RuleType  int8       `json:"rule_type"`
	Rule      *AlbumRule `json:"rule"`
	AutoAdd   *bool      `json:"auto_add"`
	SortOrder int        `json:"sort_order"`
}

// createAlbum 新建相册。带规则时可立即按规则匹配一次
func (s *Server) createAlbum(c *gin.Context) {
	var req albumReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		fail(c, http.StatusBadRequest, "请填写相册名称")
		return
	}
	now := time.Now()
	a := model.Album{
		Name: name, Remark: strings.TrimSpace(req.Remark), CoverPath: req.CoverPath,
		RuleType: model.AlbumRuleType(req.RuleType), SortOrder: req.SortOrder,
		CreatedAt: &now, UpdatedAt: &now,
	}
	if req.Rule != nil {
		a.RuleJSON = req.Rule.json()
	}
	if req.AutoAdd != nil {
		a.AutoAdd = *req.AutoAdd
	}
	if err := s.db.Create(&a).Error; err != nil {
		fail(c, 500, "创建失败: "+err.Error())
		return
	}
	matched := int64(0)
	if a.RuleType != model.AlbumRuleManual {
		matched = s.applyAlbumRule(a.ID)
	}
	s.invalidate(cache.PfxAlbum)
	ok(c, gin.H{"album": a, "matched": matched})
}

// updateAlbum 改名 / 改规则 / 改封面
func (s *Server) updateAlbum(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var a model.Album
	if err := s.db.Where("id = ? AND deleted_at IS NULL", id).First(&a).Error; err != nil {
		fail(c, 404, "相册不存在")
		return
	}
	var req albumReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	upd := map[string]interface{}{"updated_at": time.Now()}
	if strings.TrimSpace(req.Name) != "" {
		upd["name"] = strings.TrimSpace(req.Name)
	}
	if req.Remark != "" || req.Name != "" {
		upd["remark"] = strings.TrimSpace(req.Remark)
	}
	if req.CoverPath != "" {
		upd["cover_path"] = req.CoverPath
	}
	if req.RuleType > 0 || req.Rule != nil {
		upd["rule_type"] = req.RuleType
	}
	if req.Rule != nil {
		upd["rule_json"] = req.Rule.json()
	}
	if req.AutoAdd != nil {
		upd["auto_add"] = *req.AutoAdd
	}
	if req.SortOrder != 0 {
		upd["sort_order"] = req.SortOrder
	}
	if err := s.db.Model(&a).Updates(upd).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	s.db.First(&a, id)
	s.invalidate(cache.PfxAlbum)
	ok(c, gin.H{"album": a})
}

// deleteAlbum 删除相册（连同关联，绝不碰照片本身）
func (s *Server) deleteAlbum(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var a model.Album
	if err := s.db.First(&a, id).Error; err != nil {
		fail(c, 404, "相册不存在")
		return
	}
	s.db.Where("album_id = ?", id).Delete(&model.AlbumItem{})
	s.db.Delete(&a)
	s.invalidate(cache.PfxAlbum)
	ok(c, gin.H{"id": id})
}

// albumMedia 相册内的照片（复用时间轴分页参数）
func (s *Server) albumMedia(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var a model.Album
	if err := s.db.Where("id = ? AND deleted_at IS NULL", id).First(&a).Error; err != nil {
		fail(c, 404, "相册不存在")
		return
	}
	c.Request.URL.RawQuery += "&album_id=" + strconv.FormatInt(id, 10)
	s.listMedia(c)
}

type albumItemsReq struct {
	MediaIDs []int64 `json:"media_ids"`
	Rule     *AlbumRule `json:"rule"`
	Replace  bool    `json:"replace"`
}

// addAlbumItems 往相册里加照片
func (s *Server) addAlbumItems(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var a model.Album
	if err := s.db.Where("id = ? AND deleted_at IS NULL", id).First(&a).Error; err != nil {
		fail(c, 404, "相册不存在")
		return
	}
	var req albumItemsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	rows := make([]model.AlbumItem, 0, len(req.MediaIDs))
	now := time.Now()
	for _, mid := range req.MediaIDs {
		if mid <= 0 {
			continue
		}
		rows = append(rows, model.AlbumItem{AlbumID: id, MediaID: mid, CreatedAt: &now})
	}
	if len(rows) == 0 {
		fail(c, http.StatusBadRequest, "没有选择照片")
		return
	}
	// 冲突即忽略：重复加同一张不该报错
	if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	s.refreshAlbumCount(id)
	s.invalidate(cache.PfxAlbum)
	ok(c, gin.H{"added": len(rows)})
}

// removeAlbumItems 从相册移出照片（只删关联，不动照片）
func (s *Server) removeAlbumItems(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req albumItemsReq
	_ = c.ShouldBindJSON(&req)
	if len(req.MediaIDs) == 0 {
		if mid, err := strconv.ParseInt(c.Param("mediaId"), 10, 64); err == nil && mid > 0 {
			req.MediaIDs = []int64{mid}
		}
	}
	if len(req.MediaIDs) == 0 {
		fail(c, http.StatusBadRequest, "没有选择照片")
		return
	}
	res := s.db.Where("album_id = ? AND media_id IN ?", id, req.MediaIDs).
		Delete(&model.AlbumItem{})
	s.refreshAlbumCount(id)
	s.invalidate(cache.PfxAlbum)
	ok(c, gin.H{"removed": res.RowsAffected})
}

// applyAlbumRule 按规则把命中的照片并入相册（已存在则忽略）
func (s *Server) applyAlbum(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var a model.Album
	if err := s.db.Where("id = ? AND deleted_at IS NULL", id).First(&a).Error; err != nil {
		fail(c, 404, "相册不存在")
		return
	}
	// 允许请求体里临时覆盖规则，方便前端「先试试这段时间的照片有多少」
	var req struct {
		Rule    *AlbumRule `json:"rule"`
		Replace bool       `json:"replace"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Rule != nil {
		a.RuleJSON = req.Rule.json()
		s.db.Model(&model.Album{}).Where("id = ?", id).Update("rule_json", a.RuleJSON)
	}
	if req.Replace {
		s.db.Where("album_id = ?", id).Delete(&model.AlbumItem{})
	}
	n := s.applyAlbumRule(id)
	s.invalidate(cache.PfxAlbum)
	ok(c, gin.H{"matched": n, "album_id": id})
}

// applyAlbumRule 规则匹配落库，返回命中条数
func (s *Server) applyAlbumRule(albumID int64) int64 {
	var a model.Album
	if err := s.db.First(&a, albumID).Error; err != nil {
		return 0
	}
	q := s.db.Model(&model.MediaFile{}).Where("deleted_at IS NULL")
	switch a.RuleType {
	case model.AlbumRuleTime:
		r := parseRule(a.RuleJSON)
		if t := parseTimeParam(r.Start); t != nil {
			q = q.Where("taken_at >= ?", *t)
		}
		if t := parseTimeParam(r.End); t != nil {
			q = q.Where("taken_at <= ?", t.Add(24*time.Hour-time.Second))
		}
		if r.LibraryID > 0 {
			q = q.Where("library_id = ?", r.LibraryID)
		}
		if r.MediaType > 0 {
			q = q.Where("media_type = ?", r.MediaType)
		}
	case model.AlbumRuleKeyword:
		r := parseRule(a.RuleJSON)
		if strings.TrimSpace(r.Keyword) == "" {
			return 0
		}
		like := "%" + strings.ToLower(strings.TrimSpace(r.Keyword)) + "%"
		q = q.Where("(LOWER(filename) LIKE ? OR LOWER(relative_path) LIKE ?)", like, like)
		if r.LibraryID > 0 {
			q = q.Where("library_id = ?", r.LibraryID)
		}
		if r.MediaType > 0 {
			q = q.Where("media_type = ?", r.MediaType)
		}
	case model.AlbumRuleLibrary:
		r := parseRule(a.RuleJSON)
		if r.LibraryID <= 0 {
			return 0
		}
		q = q.Where("library_id = ?", r.LibraryID)
		if r.MediaType > 0 {
			q = q.Where("media_type = ?", r.MediaType)
		}
	case model.AlbumRuleFavorite:
		q = q.Where("favorite = 1")
	default:
		return 0
	}

	var ids []int64
	if err := q.Order("taken_at DESC, id DESC").Limit(20000).Pluck("id", &ids).Error; err != nil {
		return 0
	}
	if len(ids) == 0 {
		s.refreshAlbumCount(albumID)
		return 0
	}
	now := time.Now()
	rows := make([]model.AlbumItem, 0, len(ids))
	for _, mid := range ids {
		rows = append(rows, model.AlbumItem{AlbumID: albumID, MediaID: mid, CreatedAt: &now})
	}
	// 分批插入，避免超长 SQL
	for i := 0; i < len(rows); i += 500 {
		end := i + 500
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]
		_ = s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&batch).Error
	}
	s.refreshAlbumCount(albumID)
	return int64(len(ids))
}

func (s *Server) refreshAlbumCount(albumID int64) {
	var n int64
	s.db.Model(&model.AlbumItem{}).Where("album_id = ?", albumID).Count(&n)
	s.db.Model(&model.Album{}).Where("id = ?", albumID).
		Updates(map[string]interface{}{"item_count": n, "updated_at": time.Now()})
}

// mediaAlbums 某张照片属于哪些相册（详情页用）
func (s *Server) mediaAlbums(c *gin.Context) {
	mid, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var list []model.Album
	s.db.Where("id IN (SELECT album_id FROM album_items WHERE media_id = ?) AND deleted_at IS NULL", mid).
		Order("sort_order ASC, id ASC").Find(&list)
	ok(c, gin.H{"items": list})
}

// EnsureDefaultAlbums 启动兜底：首次进入相册页时给点默认相册，避免空空如也
func EnsureDefaultAlbums(db *gorm.DB) {
	(&Server{db: db}).ensureDefaultAlbums()
}

func (s *Server) ensureDefaultAlbums() {
	var n int64
	s.db.Model(&model.Album{}).Where("deleted_at IS NULL").Count(&n)
	if n > 0 {
		return
	}
	var favCount int64
	s.db.Model(&model.MediaFile{}).Where("deleted_at IS NULL AND favorite = 1").Count(&favCount)
	seeds := []model.Album{
		{Name: "今年的照片", RuleType: model.AlbumRuleTime,
			RuleJSON: AlbumRule{Start: time.Now().Format("2006") + "-01-01"}.json(), AutoAdd: true},
	}
	now := time.Now()
	for i := range seeds {
		seeds[i].CreatedAt = &now
		seeds[i].UpdatedAt = &now
		if err := s.db.Create(&seeds[i]).Error; err == nil {
			s.applyAlbumRule(seeds[i].ID)
		}
	}
	// 老收藏数据：有就迁成一个「我喜欢的」相册，没有就不建
	if favCount > 0 {
		a := model.Album{Name: "我喜欢的", RuleType: model.AlbumRuleFavorite,
			Remark: "从旧版收藏迁移而来", CreatedAt: &now, UpdatedAt: &now}
		if err := s.db.Create(&a).Error; err == nil {
			s.applyAlbumRule(a.ID)
		}
	}
}
