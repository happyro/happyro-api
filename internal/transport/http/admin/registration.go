package admin

import (
	"errors"

	"github.com/gin-gonic/gin"
	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/transport/http/response"
)

func (s *Handler) readRegistrationSettings(c *gin.Context) {
	result, err := s.AccountManagement.RegistrationSettings(c.Request.Context())
	if err != nil {
		s.InternalError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (s *Handler) updateRegistrationSettings(c *gin.Context) {
	var input accountsapi.RegistrationUpdate
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "注册设置参数格式无效。")
		return
	}
	row, err := s.AccountManagement.UpdateRegistration(c.Request.Context(), input, c.MustGet("user").(identityapi.User), response.Client(c))
	switch {
	case errors.Is(err, accountsapi.ErrRegistrationLocked):
		s.Fail(c, 403, "registration_settings_locked", err.Error())
	case errors.Is(err, accountsapi.ErrGroupChanged):
		s.Fail(c, 409, "registration_settings_changed", err.Error())
	case err != nil:
		s.accountError(c, err)
	default:
		c.JSON(200, gin.H{"data": row})
	}
}
