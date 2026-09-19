package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/naspic/naspic/internal/model"
)

// Auth 轻量认证：登录换 token，token 存内存（重启失效）。
// 生产环境可替换为 JWT，接口契约不变。
type Auth struct {
	db     *gorm.DB
	mu     sync.RWMutex
	tokens map[string]int64 // token -> user_id
}

func NewAuth(db *gorm.DB) *Auth {
	return &Auth{db: db, tokens: make(map[string]int64)}
}

func (a *Auth) login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	var u model.User
	if err := a.db.Where("username = ? AND deleted_at IS NULL", req.Username).First(&u).Error; err != nil {
		fail(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		fail(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	token, err := randomToken()
	if err != nil {
		fail(c, 500, "生成令牌失败")
		return
	}
	a.mu.Lock()
	a.tokens[token] = u.ID
	a.mu.Unlock()

	now := time.Now()
	a.db.Model(&model.User{}).Where("id = ?", u.ID).Update("last_login_at", now)
	now2 := time.Now()
	a.db.Model(&model.User{}).Where("id = ?", u.ID).Update("updated_at", now2)

	ok(c, gin.H{"token": token, "user_id": u.ID, "role": u.Role})
}

// Middleware 校验 Bearer Token
func (a *Auth) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if token == "" {
			token = c.GetHeader("X-Naspic-Token")
		}
		if token == "" {
			// 图片/视频的 <img src>、<video src> 无法自定义请求头，
			// 因此允许通过 query 参数携带 token（仅用于媒体直链）
			token = c.Query("token")
		}
		if token == "" {
			fail(c, http.StatusUnauthorized, "未登录")
			c.Abort()
			return
		}
		a.mu.RLock()
		uid, exist := a.tokens[token]
		a.mu.RUnlock()
		if !exist {
			fail(c, http.StatusUnauthorized, "令牌无效或已过期")
			c.Abort()
			return
		}
		c.Set("user_id", uid)
		c.Next()
	}
}

// CurrentUserID 取当前用户 ID
func CurrentUserID(c *gin.Context) int64 {
	v, _ := c.Get("user_id")
	uid, _ := v.(int64)
	return uid
}

// HashPassword 生成 bcrypt 密码哈希
func HashPassword(pwd string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// EnsureDefaultAdmin 首次启动创建默认管理员
func EnsureDefaultAdmin(db *gorm.DB) error {
	var n int64
	if err := db.Model(&model.User{}).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := HashPassword("naspic123")
	if err != nil {
		return err
	}
	now := time.Now()
	return db.Create(&model.User{
		Username: "admin", PasswordHash: hash, Nickname: "管理员",
		Role: 1, Status: 1, CreatedAt: &now, UpdatedAt: &now,
	}).Error
}

var ErrNoUser = errors.New("未找到用户")

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
