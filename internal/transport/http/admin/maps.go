package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/happyro/happyro-api/internal/catalog"
	"net/http"
)

func (s *Handler) maps(c *gin.Context) {
	pagination, ok := s.Pagination(c)
	if !ok {
		return
	}
	scope := c.DefaultQuery("scope", "game")
	if scope != "game" && scope != "all" {
		s.Fail(c, 422, "validation_failed", "地图范围必须是 game 或 all。")
		return
	}
	if len([]rune(c.Query("query"))) > 100 {
		s.Fail(c, 422, "validation_failed", "地图查询条件过长。")
		return
	}
	channels := false
	if scope == "game" {
		values, err := s.GameGateway.BattleConfig(c.Request.Context())
		if err != nil {
			s.Fail(c, http.StatusServiceUnavailable, "game_settings_unavailable", "暂时无法读取游戏地图配置，请重试或查看全部服务端地图。")
			return
		}
		channels = values["navigation_map_channels_enabled"] != 0
	}
	page, err := s.Maps.Maps(catalog.MapQuery{GameOnly: scope == "game", ChannelsEnabled: channels, Search: c.Query("query"), Pagination: pagination})
	if err != nil {
		s.InternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": page.Data, "total": page.Total, "success": true})
}
func (s *Handler) mapImage(c *gin.Context) {
	path, err := s.Maps.MapImage(c.Param("map"))
	if err != nil {
		s.Fail(c, 404, "not_found", "地图图片不存在。")
		return
	}
	s.Asset(c, s.Config.WorldAssetRoot, path)
}
