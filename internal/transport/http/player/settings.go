package player

import (
	"github.com/gin-gonic/gin"
	gameapi "github.com/happyro/happyro-api/internal/game"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"github.com/happyro/happyro-api/internal/settings"
	"github.com/happyro/happyro-api/internal/transport/http/response"
)

func (s *Handler) readSettings(c *gin.Context) {
	values, err := s.GameGateway.BattleConfig(c.Request.Context())
	if err != nil {
		s.PlayerError(c, err)
		return
	}
	if !sessionauth.Allows(values["game_tools_game_settings_policy"], gamePrincipal(c)) {
		s.PlayerError(c, sessionauth.ErrPlayerForbidden)
		return
	}
	definitions := settings.Definitions(true)
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
	principal := gamePrincipal(c)
	if err := s.PlayerAccess.Authorize(c.Request.Context(), "game_tools_game_settings_policy", principal); err != nil {
		s.PlayerError(c, err)
		return
	}
	actor.GameAccountID = &principal.AccountID

	_, err := s.Settings.Apply(c.Request.Context(), input, actor)
	if err != nil {
		s.SettingError(c, err)
		return
	}
	s.readSettings(c)
}
