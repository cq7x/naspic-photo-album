package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/naspic/naspic/internal/model"
)

// 管理员账号设置：列表 / 新增 / 改资料 / 改密码 / 启用禁用 / 删除。
// 只有 role=1（管理员）能操作；且不允许把自己删掉或把自己降权，
// 否则会出现「系统没有任何管理员」的死锁。

type adminUserOut struct {
	ID         int64      `json:"id"`
	Username   string     `json:"username"`
	Nickname   string     `json:"nickname"`
	Role       int8       `json:"role"`
	Status     int8       `json:"status"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt  *time.Time `json:"created_at"`
}

func (s *Server) requireAdmin(c *gin.Context) bool {
	if CurrentUserID(c) == 0 {
		fail(c, http.StatusUnauthorized, "未登录")
		return false
	}
	var u model.User
	if err := s.db.Select("id, role, status").First(&u, CurrentUserID(c)).Error; err != nil {
		fail(c, http.StatusUnauthorized, "账号不存在")
		return false
	}
	if u.Role != 1 {
		fail(c, http.StatusForbidden, "只有管理员可以操作账号")
		return false
	}
	return true
}

// listAdminUsers 账号列表
func (s *Server) listAdminUsers(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	var list []model.User
	if err := s.db.Where("deleted_at IS NULL").
		Order("role ASC, id ASC").Find(&list).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	out := make([]adminUserOut, 0, len(list))
	for _, u := range list {
		out = append(out, adminUserOut{
			ID: u.ID, Username: u.Username, Nickname: u.Nickname, Role: u.Role,
			Status: u.Status, LastLoginAt: u.LastLoginAt, CreatedAt: u.CreatedAt,
		})
	}
	ok(c, gin.H{"items": out})
}

// createAdminUser 新增账号
func (s *Server) createAdminUser(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Nickname string `json:"nickname"`
		Role     int8   `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	name := strings.TrimSpace(req.Username)
	if len([]rune(name)) < 3 {
		fail(c, http.StatusBadRequest, "用户名至少 3 个字符")
		return
	}
	if len(req.Password) < 6 {
		fail(c, http.StatusBadRequest, "密码至少 6 位")
		return
	}
	if req.Role != 1 && req.Role != 2 {
		req.Role = 2
	}
	var dup int64
	s.db.Model(&model.User{}).Where("username = ? AND deleted_at IS NULL", name).Count(&dup)
	if dup > 0 {
		fail(c, http.StatusConflict, "用户名已存在")
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		fail(c, 500, "密码加密失败")
		return
	}
	now := time.Now()
	u := model.User{
		Username: name, PasswordHash: hash, Nickname: strings.TrimSpace(req.Nickname),
		Role: req.Role, Status: 1, CreatedAt: &now, UpdatedAt: &now,
	}
	if u.Nickname == "" {
		u.Nickname = name
	}
	if err := s.db.Create(&u).Error; err != nil {
		fail(c, 500, "创建失败: "+err.Error())
		return
	}
	ok(c, gin.H{"id": u.ID})
}

// updateAdminUser 改昵称 / 角色 / 启用禁用
func (s *Server) updateAdminUser(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var u model.User
	if err := s.db.Where("id = ? AND deleted_at IS NULL", id).First(&u).Error; err != nil {
		fail(c, 404, "账号不存在")
		return
	}
	var req struct {
		Nickname *string `json:"nickname"`
		Role     *int8   `json:"role"`
		Status   *int8   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	me := CurrentUserID(c)
	upd := map[string]interface{}{"updated_at": time.Now()}
	if req.Nickname != nil {
		upd["nickname"] = strings.TrimSpace(*req.Nickname)
	}
	if req.Role != nil {
		if id == me && *req.Role != 1 {
			fail(c, http.StatusBadRequest, "不能把自己的管理员权限取消")
			return
		}
		// 最后一个管理员不允许被降权
		if *req.Role != 1 && u.Role == 1 && s.adminCount() <= 1 {
			fail(c, http.StatusBadRequest, "至少要保留一个管理员")
			return
		}
		upd["role"] = *req.Role
	}
	if req.Status != nil {
		if id == me && *req.Status != 1 {
			fail(c, http.StatusBadRequest, "不能禁用当前登录的账号")
			return
		}
		upd["status"] = *req.Status
	}
	if err := s.db.Model(&u).Updates(upd).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

// resetAdminPassword 管理员给别人重置密码
func (s *Server) resetAdminPassword(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(req.Password) < 6 {
		fail(c, http.StatusBadRequest, "新密码至少 6 位")
		return
	}
	var u model.User
	if err := s.db.Where("id = ? AND deleted_at IS NULL", id).First(&u).Error; err != nil {
		fail(c, 404, "账号不存在")
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		fail(c, 500, "密码加密失败")
		return
	}
	s.db.Model(&u).Updates(map[string]interface{}{
		"password_hash": hash, "updated_at": time.Now(),
	})
	ok(c, gin.H{"id": id})
}

// deleteAdminUser 删除账号（软删）
func (s *Server) deleteAdminUser(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id == CurrentUserID(c) {
		fail(c, http.StatusBadRequest, "不能删除当前登录的账号")
		return
	}
	var u model.User
	if err := s.db.Where("id = ? AND deleted_at IS NULL", id).First(&u).Error; err != nil {
		fail(c, 404, "账号不存在")
		return
	}
	if u.Role == 1 && s.adminCount() <= 1 {
		fail(c, http.StatusBadRequest, "至少要保留一个管理员")
		return
	}
	now := time.Now()
	s.db.Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]interface{}{"deleted_at": now, "updated_at": now})
	ok(c, gin.H{"id": id})
}

func (s *Server) adminCount() int64 {
	var n int64
	s.db.Model(&model.User{}).Where("role = 1 AND deleted_at IS NULL").Count(&n)
	return n
}

// changeMyPassword 当前用户修改自己的密码
func (s *Server) changeMyPassword(c *gin.Context) {
	uid := CurrentUserID(c)
	if uid == 0 {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(req.NewPassword) < 6 {
		fail(c, http.StatusBadRequest, "新密码至少 6 位")
		return
	}
	var u model.User
	if err := s.db.Where("id = ? AND deleted_at IS NULL", uid).First(&u).Error; err != nil {
		fail(c, 404, "账号不存在")
		return
	}
	if err := comparePassword(u.PasswordHash, req.OldPassword); err != nil {
		fail(c, http.StatusBadRequest, "原密码不正确")
		return
	}
	hash, err := HashPassword(req.NewPassword)
	if err != nil {
		fail(c, 500, "密码加密失败")
		return
	}
	s.db.Model(&u).Updates(map[string]interface{}{
		"password_hash": hash, "updated_at": time.Now(),
	})
	ok(c, gin.H{"ok": true})
}

// myProfile 当前登录账号信息
func (s *Server) myProfile(c *gin.Context) {
	uid := CurrentUserID(c)
	if uid == 0 {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var u model.User
	if err := s.db.Where("id = ? AND deleted_at IS NULL", uid).First(&u).Error; err != nil {
		fail(c, 404, "账号不存在")
		return
	}
	ok(c, gin.H{
		"id": u.ID, "username": u.Username, "nickname": u.Nickname,
		"role": u.Role, "status": u.Status, "last_login_at": u.LastLoginAt,
	})
}

var errBadPassword = errors.New("密码错误")

func comparePassword(hash, pwd string) error {
	if hash == "" {
		return errBadPassword
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pwd)); err != nil {
		return errBadPassword
	}
	return nil
}
