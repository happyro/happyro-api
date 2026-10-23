package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"github.com/gin-gonic/gin"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"github.com/happyro/happyro-api/internal/transport/http/response"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func passwordVersion(hash string) string {
	sum := sha256.Sum256([]byte(hash))
	return hex.EncodeToString(sum[:])
}

func (s *Handler) csrf(c *gin.Context) {
	ctx := c.Request.Context()
	token := s.Sessions.GetString(ctx, "csrf_token")
	if token == "" {
		bytes := make([]byte, 32)
		if _, err := rand.Read(bytes); err != nil {
			s.InternalError(c, err)
			return
		}
		token = hex.EncodeToString(bytes)
		s.Sessions.Put(ctx, "csrf_token", token)
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: "XSRF-TOKEN", Value: token, Path: "/", Secure: s.Config.SecureCookies, HttpOnly: false, SameSite: http.SameSiteLaxMode})
	c.Status(http.StatusNoContent)
}

func (s *Handler) validCSRF(c *gin.Context) bool {
	expected := s.Sessions.GetString(c.Request.Context(), "csrf_token")
	actual := c.GetHeader("X-XSRF-TOKEN")
	return len(expected) == 64 && subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func (s *Handler) login(c *gin.Context) {
	var input struct {
		Username string `json:"username" binding:"required,max=64"`
		Password string `json:"password" binding:"required,max=255"`
		Remember bool   `json:"remember"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		s.Fail(c, 422, "validation_failed", "请输入有效的用户名和密码。")
		return
	}
	user, err := s.Auth.Login(c.Request.Context(), input.Username, input.Password, response.Client(c))
	if err != nil {
		var limited *sessionauth.RateLimited
		switch {
		case errors.As(err, &limited):
			c.Header("Retry-After", strconv.Itoa(limited.RetryAfter))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"message": limited.Error(), "retry_after": limited.RetryAfter})
		case errors.Is(err, sessionauth.ErrCredentials):
			s.Fail(c, 422, "invalid_credentials", err.Error())
		default:
			s.InternalError(c, err)
		}
		return
	}
	ctx := c.Request.Context()
	if err = s.Sessions.RenewToken(ctx); err != nil {
		s.InternalError(c, err)
		return
	}
	s.Sessions.Put(ctx, "user_id", user.ID)
	s.Sessions.Put(ctx, "password_version", passwordVersion(user.PasswordHash))
	lifetime := s.Config.SessionLifetime
	if input.Remember {
		lifetime = s.Config.RememberLifetime
	}
	s.Sessions.SetDeadline(ctx, time.Now().Add(lifetime))
	s.Sessions.RememberMe(ctx, input.Remember)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Handler) requireUser(c *gin.Context) {
	ctx := c.Request.Context()
	id := s.Sessions.GetInt64(ctx, "user_id")
	if id == 0 {
		s.Fail(c, http.StatusUnauthorized, "unauthenticated", "请先登录。")
		return
	}
	user, err := s.Auth.Current(ctx, id)
	if errors.Is(err, identityapi.ErrNotFound) || errors.Is(err, sessionauth.ErrDisabled) {
		if err = s.Sessions.Destroy(ctx); err != nil {
			s.InternalError(c, err)
			return
		}
		s.Fail(c, http.StatusUnauthorized, "unauthenticated", "请重新登录。")
		return
	}
	if err != nil {
		s.InternalError(c, err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(s.Sessions.GetString(ctx, "password_version")), []byte(passwordVersion(user.PasswordHash))) != 1 {
		if err = s.Sessions.Destroy(ctx); err != nil {
			s.InternalError(c, err)
			return
		}
		s.Fail(c, http.StatusUnauthorized, "unauthenticated", "密码已更新，请重新登录。")
		return
	}
	c.Set("user", user)
	c.Next()
}

func (s *Handler) permission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := c.MustGet("user").(identityapi.User)
		if !ok || !user.Can(permission) {
			s.Fail(c, http.StatusForbidden, "forbidden", "没有执行此操作的权限。")
			return
		}
		c.Next()
	}
}

func (s *Handler) current(c *gin.Context) {
	user := c.MustGet("user").(identityapi.User)
	access := "user"
	if user.Can("*") {
		access = "admin"
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": user.ID, "userid": user.Username, "username": user.Username, "name": user.Name, "access": access, "roles": user.Roles, "permissions": user.Permissions}})
}

func (s *Handler) logout(c *gin.Context) {
	user := c.MustGet("user").(identityapi.User)
	if err := s.Auth.Logout(c.Request.Context(), user, response.Client(c)); err != nil {
		s.InternalError(c, err)
		return
	}
	if err := s.Sessions.Destroy(c.Request.Context()); err != nil {
		s.InternalError(c, err)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: "XSRF-TOKEN", Path: "/", MaxAge: -1, Secure: s.Config.SecureCookies, SameSite: http.SameSiteLaxMode})
	c.Status(http.StatusNoContent)
}
func (s *Handler) Protect(c *gin.Context) {
	c.Set("request_id", c.Writer.Header().Get("X-Request-ID"))
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		c.Next()
		return
	}
	if s.Config.AdminCSRFOriginCheckEnabled {
		if origin := c.GetHeader("Origin"); origin != "" {
			expected, _ := url.Parse(s.Config.PublicURL)
			if origin != expected.Scheme+"://"+expected.Host {
				s.Fail(c, http.StatusForbidden, "csrf_failed", "请求来源不受信任。")
				return
			}
		}
		if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
			s.Fail(c, http.StatusForbidden, "csrf_failed", "请求来源不受信任。")
			return
		}
	}
	if !s.validCSRF(c) {
		s.Fail(c, 419, "csrf_failed", "会话验证已过期，请刷新页面后重试。")
		return
	}
	c.Next()
}
