package admin

import (
	"errors"
	"github.com/gin-gonic/gin"
	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	charactersapi "github.com/happyro/happyro-api/internal/characters"
	"net/http"
	"strconv"
)

func (s *Handler) playerError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, charactersapi.ErrNotFound):
		s.Fail(c, 404, "character_not_found", err.Error())
	case errors.Is(err, accountsapi.ErrFilter), errors.Is(err, charactersapi.ErrFilter):
		s.Fail(c, 422, "validation_failed", err.Error())
	default:
		s.InternalError(c, err)
	}
}
func (s *Handler) accounts(c *gin.Context) {
	pagination, ok := s.Pagination(c)
	if !ok {
		return
	}
	page, err := s.Accounts.Accounts(c.Request.Context(), accountsapi.AccountQuery{Username: c.Query("username"), Email: c.Query("email"), State: c.Query("state"), Pagination: pagination})
	if err != nil {
		s.playerError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}
func (s *Handler) characters(c *gin.Context) {
	pagination, ok := s.Pagination(c)
	if !ok {
		return
	}
	var filters struct {
		Search     string `form:"query"`
		OnlineOnly bool   `form:"onlineOnly"`
	}
	if err := c.ShouldBindQuery(&filters); err != nil {
		s.playerError(c, charactersapi.ErrFilter)
		return
	}
	page, err := s.Characters.Characters(c.Request.Context(), charactersapi.Query{Search: filters.Search, OnlineOnly: filters.OnlineOnly, Username: c.Query("username"), Name: c.Query("name"), Pagination: pagination})
	if err != nil {
		s.playerError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}
func (s *Handler) character(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("charId"), 10, 64)
	if err != nil || id <= 0 {
		s.Fail(c, 404, "character_not_found", "角色不存在。")
		return
	}
	row, err := s.Characters.Character(c.Request.Context(), id)
	if err != nil {
		s.playerError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": row})
}
func (s *Handler) loginLogs(c *gin.Context) {
	pagination, ok := s.Pagination(c)
	if !ok {
		return
	}
	page, err := s.Accounts.LoginLogs(c.Request.Context(), accountsapi.LoginLogQuery{Username: c.Query("username"), Pagination: pagination})
	if err != nil {
		s.playerError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}
