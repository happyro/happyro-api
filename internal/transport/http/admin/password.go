package admin

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/transport/http/response"
	"net/http"
	"strconv"
)

func (s *Handler) changePassword(c *gin.Context) {
	if !s.Config.AdminPasswordChangeEnabled {
		s.Fail(c, 403, "password_change_disabled", "修改密码功能已关闭。")
		return
	}
	var input struct {
		Current      string `json:"currentPassword" binding:"required,max=255"`
		Password     string `json:"newPassword" binding:"required,max=255"`
		Confirmation string `json:"confirmPassword" binding:"required,max=255"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		s.Fail(c, 422, "validation_failed", "请填写当前密码和新密码，密码不能超过 255 字节。")
		return
	}
	user := c.MustGet("user").(identity.User)
	err := s.Passwords.Change(c.Request.Context(), user.ID, input.Current, input.Password, input.Confirmation, response.Client(c))
	if err != nil {
		var limited *identity.PasswordRateLimited
		switch {
		case errors.As(err, &limited):
			c.Header("Retry-After", strconv.Itoa(limited.RetryAfter))
			s.Fail(c, 429, "password_rate_limited", limited.Error())
		case errors.Is(err, identity.ErrCurrentPassword), errors.Is(err, identity.ErrPasswordInput):
			s.Fail(c, 422, "password_change_failed", err.Error())
		default:
			s.InternalError(c, err)
		}
		return
	}
	if err = s.Sessions.Destroy(c.Request.Context()); err != nil {
		s.InternalError(c, err)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: "XSRF-TOKEN", Path: "/", MaxAge: -1, Secure: s.Config.SecureCookies, SameSite: http.SameSiteLaxMode})
	c.Status(http.StatusNoContent)
}
