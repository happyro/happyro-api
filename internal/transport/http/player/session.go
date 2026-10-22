package player

import (
	"errors"
	"github.com/gin-gonic/gin"
	charactersapi "github.com/happyro/happyro-api/internal/characters"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"github.com/happyro/happyro-api/internal/transport/http/response"
	"strconv"
)

func (s *Handler) requireGameSession(c *gin.Context) {
	account, accountErr := strconv.ParseInt(c.GetHeader("X-HappyRO-Account-ID"), 10, 32)
	character, characterErr := strconv.ParseInt(c.GetHeader("X-HappyRO-Character-ID"), 10, 32)
	if accountErr != nil || characterErr != nil {
		s.Fail(c, 401, "invalid_game_session", sessionauth.ErrGameSession.Error())
		return
	}
	principal, err := s.GameAuth.Authenticate(c.Request.Context(), account, character, c.GetHeader("X-HappyRO-Auth-Token"))
	if err != nil {
		if errors.Is(err, sessionauth.ErrGameSession) {
			s.Fail(c, 401, "invalid_game_session", sessionauth.ErrGameSession.Error())
		} else {
			s.InternalError(c, err)
		}
		return
	}
	c.Set("game_principal", principal)
	c.Next()
}
func gamePrincipal(c *gin.Context) sessionauth.GamePrincipal {
	return c.MustGet("game_principal").(sessionauth.GamePrincipal)
}
func (s *Handler) bootstrap(c *gin.Context) {
	result, err := s.PlayerAccess.Bootstrap(c.Request.Context(), gamePrincipal(c))
	if err != nil {
		s.PlayerError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}
func (s *Handler) character(c *gin.Context) {
	result, err := s.PlayerCharacters.Character(c.Request.Context(), gamePrincipal(c))
	if err != nil {
		s.PlayerError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}
func (s *Handler) allocationLimits(c *gin.Context) {
	result, err := s.PlayerCharacters.AllocationLimits(c.Request.Context(), gamePrincipal(c))
	if err != nil {
		s.PlayerError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (s *Handler) maintain(c *gin.Context) {
	var input charactersapi.Maintenance
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "角色维护参数格式无效。")
		return
	}
	result, err := s.PlayerCharacters.Maintain(c.Request.Context(), gamePrincipal(c), input)
	if err != nil {
		s.PlayerError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (s *Handler) rateLimit(category string, limit int) gin.HandlerFunc {
	return func(c *gin.Context) {
		retry, err := s.PlayerLimits.Allow(c.Request.Context(), gamePrincipal(c).AccountID, category, limit)
		if err != nil {
			s.InternalError(c, err)
			return
		}
		if retry > 0 {
			c.Header("Retry-After", strconv.Itoa(retry))
			s.Fail(c, 429, "rate_limited", "请求过于频繁，请稍后再试。")
			return
		}
		c.Next()
	}
}
