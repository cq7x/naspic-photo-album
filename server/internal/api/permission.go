package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/naspic/naspic/internal/model"
)

// 权限：支持把相册库 / 挂载目录授权给多个用户或多个群组

// listPermissions 权限列表（按资源过滤）
func (s *Server) listPermissions(c *gin.Context) {
	rt, _ := strconv.Atoi(c.Query("resource_type"))
	rid, _ := strconv.ParseInt(c.Query("resource_id"), 10, 64)
	q := s.db.Order("id DESC")
	if rt > 0 {
		q = q.Where("resource_type = ?", rt)
	}
	if rid > 0 {
		q = q.Where("resource_id = ?", rid)
	}
	var list []model.Permission
	if err := q.Find(&list).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, list)
}

type permissionReq struct {
	ID           int64 `json:"id"`
	SubjectType  int8  `json:"subject_type"`  // 1=用户 2=群组
	SubjectID    int64 `json:"subject_id"`
	ResourceType int8  `json:"resource_type"` // 1=相册库 2=挂载目录
	ResourceID   int64 `json:"resource_id"`
	Permission   int8  `json:"permission"` // 1=只读 2=上传 3=管理
}

// upsertPermission 新增或更新授权
func (s *Server) upsertPermission(c *gin.Context) {
	var req permissionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if req.SubjectID == 0 || req.ResourceID == 0 || req.ResourceType == 0 {
		fail(c, http.StatusBadRequest, "subject_id / resource_type / resource_id 必填")
		return
	}
	if req.Permission == 0 {
		req.Permission = 1
	}
	now := time.Now()
	p := model.Permission{
		SubjectType: req.SubjectType, SubjectID: req.SubjectID,
		ResourceType: req.ResourceType, ResourceID: req.ResourceID,
		Permission: req.Permission, CreatedAt: &now,
	}
	// 同一主体+资源只保留一条：先删后插，兼容 SQLite/MySQL
	s.db.Where("subject_type = ? AND subject_id = ? AND resource_type = ? AND resource_id = ?",
		req.SubjectType, req.SubjectID, req.ResourceType, req.ResourceID).
		Delete(&model.Permission{})
	if err := s.db.Create(&p).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, p)
}

func (s *Server) deletePermission(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := s.db.Delete(&model.Permission{}, id).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

// listUsers 用户列表（授权主体选择器用）
func (s *Server) listUsers(c *gin.Context) {
	var list []model.User
	if err := s.db.Select("id, username, nickname, role").
		Where("deleted_at IS NULL").Find(&list).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, list)
}

func (s *Server) listGroups(c *gin.Context) {
	var list []model.Group
	if err := s.db.Where("deleted_at IS NULL").Find(&list).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, list)
}

func (s *Server) createGroup(c *gin.Context) {
	var req struct {
		Name   string `json:"name"`
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		fail(c, http.StatusBadRequest, "名称必填")
		return
	}
	now := time.Now()
	g := model.Group{Name: req.Name, Remark: req.Remark, CreatedAt: &now, UpdatedAt: &now}
	if err := s.db.Create(&g).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, g)
}
