package admin

import (
	"github.com/gin-gonic/gin"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/settings"
	"github.com/happyro/happyro-api/internal/transport/http/response"
)

func (s *Handler) readSettings(c *gin.Context) {
	values, err := s.GameGateway.BattleConfig(c.Request.Context())
	if err != nil {
		s.PlayerError(c, err)
		return
	}
	definitions := settings.Definitions(false)
	exposed := make(map[string]int64, len(definitions))
	for key := range definitions {
		if value, ok := values[key]; ok {
			exposed[key] = value
		}
	}
	c.JSON(200, gin.H{"data": gin.H{"values": exposed, "definitions": definitions}, "success": true})
}

func (s *Handler) applySettings(c *gin.Context) {
	var input settings.Update
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "游戏设置参数格式无效。")
		return
	}
	actor := gameapi.Actor{}
	user := c.MustGet("user").(identityapi.User)
	actor.AdminUserID = &user.ID

	revision, err := s.Settings.Apply(c.Request.Context(), input, actor)
	if err != nil {
		s.SettingError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": revision, "success": true})
}
func (s *Handler) gameSettingsHistory(c *gin.Context) {
	page, ok := s.PaginationWithSize(c, "per_page")
	if !ok {
		return
	}
	result, err := s.SettingsHistory.History(c.Request.Context(), page.Page, page.Size)
	if err != nil {
		s.InternalError(c, err)
		return
	}
	c.JSON(200, result)
}

func (s *Handler) resumeSettings(c *gin.Context) {
	result, err := s.SettingsRecovery.Resume(c.Request.Context(), c.Param("revisionId"), c.MustGet("user").(identityapi.User), response.Client(c))
	if err != nil {
		s.SettingError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result, "success": true})
}
