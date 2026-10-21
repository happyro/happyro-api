package player

import (
	"errors"
	"github.com/gin-gonic/gin"
	rewardsapi "github.com/happyro/happyro-api/internal/rewards"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"github.com/happyro/happyro-api/internal/transport/http/response"
)

func (s *Handler) grantError(c *gin.Context, err error) {
	if errors.Is(err, sessionauth.ErrPlayerForbidden) {
		s.PlayerError(c, err)
		return
	}
	s.GrantError(c, err)
}
func (s *Handler) grantItem(c *gin.Context) {
	var input rewardsapi.ItemGrant
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "物品发放参数格式无效。")
		return
	}
	result, err := s.PlayerRewards.GrantItem(c.Request.Context(), gamePrincipal(c), input, response.Client(c))
	if err != nil {
		s.grantError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}
func (s *Handler) grantZeny(c *gin.Context) {
	var input rewardsapi.ZenyGrant
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "金币发放参数格式无效。")
		return
	}
	result, err := s.PlayerRewards.GrantZeny(c.Request.Context(), gamePrincipal(c), input, response.Client(c))
	if err != nil {
		s.grantError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}
