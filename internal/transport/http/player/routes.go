package player

import (
	"github.com/gin-gonic/gin"
)

// Register installs game identity authentication and player limits.
func (s *Handler) Register(router *gin.Engine) {
	tools := router.Group("/api/adventure-tools", s.requireGameSession)
	tools.POST("/items/grants", s.rateLimit("action", 30), s.grantItem)
	tools.POST("/currency/zeny/grants", s.rateLimit("action", 30), s.grantZeny)
	tools.GET("/items", s.rateLimit("read", 120), s.items)
	tools.GET("/items/:itemId", s.rateLimit("read", 120), s.item)
	tools.GET("/items/:itemId/icon", s.rateLimit("assets", 600), s.CatalogAsset("icon", "itemId"))
	tools.GET("/items/:itemId/illustration", s.rateLimit("assets", 600), s.CatalogAsset("illustration", "itemId"))
	tools.GET("/npcs", s.rateLimit("read", 120), s.npcs)
	tools.GET("/maps", s.rateLimit("read", 120), s.maps)
	tools.GET("/maps/:map/npcs", s.rateLimit("read", 120), s.mapNPCs)
	tools.PUT("/game-settings", s.rateLimit("action", 30), s.applySettings)
	tools.GET("/game-settings", s.rateLimit("read", 120), s.readSettings)
	tools.GET("/bootstrap", s.rateLimit("read", 120), s.bootstrap)
	tools.POST("/character/commands", s.rateLimit("action", 30), s.maintain)
	tools.GET("/character", s.rateLimit("read", 120), s.character)
	tools.GET("/character/allocation-limits", s.rateLimit("read", 120), s.allocationLimits)
}
