package admin

import (
	"github.com/gin-gonic/gin"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/transport/http/response"
	"net/http"
	"strconv"
)

func (s *Handler) capabilities(c *gin.Context) {
	result, err := s.GameGateway.Capabilities(c.Request.Context())
	if err != nil {
		s.GameError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result, "success": true})
}
func (s *Handler) battleConfig(c *gin.Context) {
	result, err := s.GameGateway.BattleConfig(c.Request.Context())
	if err != nil {
		s.GameError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"values": result}, "success": true})
}
func (s *Handler) characterSnapshot(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("characterId"), 10, 32)
	if err != nil || id <= 0 {
		s.Fail(c, 404, "character_not_found", "角色不存在。")
		return
	}
	result, err := s.Snapshots.Read(c.Request.Context(), id)
	if err != nil {
		s.GameError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result, "success": true})
}
func (s *Handler) submitCommand(c *gin.Context) {
	var input gameapi.Request
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "游戏命令 JSON 格式无效。")
		return
	}
	user := c.MustGet("user").(identityapi.User)
	command, created, err := s.GameCommands.SubmitOperator(c.Request.Context(), input, user.ID)
	if err != nil {
		s.GameError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusAccepted
	}
	c.JSON(status, gin.H{"data": command, "success": true})
}
func (s *Handler) showCommand(c *gin.Context) {
	id := c.Param("commandId")
	if len(id) != 36 {
		s.Fail(c, 404, "not_found", "游戏命令不存在。")
		return
	}
	command, err := s.GameCommands.Commands.Find(c.Request.Context(), id)
	if err != nil {
		s.GameError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": command, "success": true})
}
