package player

import (
	"github.com/gin-gonic/gin"
	"github.com/happyro/happyro-api/internal/catalog"
	"github.com/happyro/happyro-api/internal/platform/database"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (s *Handler) page(c *gin.Context, defaultSize int) (database.Pagination, bool) {
	page, e1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("perPage", strconv.Itoa(defaultSize)))
	p := database.Pagination{Page: page, Size: size}
	if e1 != nil || e2 != nil || !p.Valid() || size > 50 {
		s.Fail(c, 422, "validation_failed", "页码或每页数量无效，每页最多 50 条。")
		return p, false
	}
	for key, max := range map[string]int{"query": 100, "onMap": 64, "currentMap": 64} {
		value := c.Query(key)
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
			s.Fail(c, 422, "validation_failed", "查询条件过长或格式无效。")
			return p, false
		}
	}
	return p, true
}
func itemAssets(row catalog.Object) {
	for _, key := range []string{"icon", "illustration"} {
		path, ok := row[key].(string)
		if ok && strings.HasPrefix(path, "/api/game-data/items/") {
			row[key] = strings.Replace(path, "/api/game-data/items/", "/api/adventure-tools/items/", 1)
		} else {
			row[key] = nil
		}
	}
	row["grantable"] = row["Type"] != "PetEgg"
}
func (s *Handler) items(c *gin.Context) {
	p, ok := s.page(c, 30)
	if !ok {
		return
	}
	page, err := s.Catalog.Items(c.Request.Context(), catalog.ItemQuery{Search: c.Query("query"), Type: c.Query("type"), Subtype: c.Query("subtype"), Range: "client", DropKind: c.DefaultQuery("dropKind", "all"), Pagination: p})
	if err != nil {
		s.CatalogError(c, err)
		return
	}
	for _, row := range page.Data {
		itemAssets(row)
	}
	c.JSON(200, page)
}
func (s *Handler) item(c *gin.Context) {
	id, ok := s.CatalogID(c, "itemId")
	if !ok {
		return
	}
	row, err := s.Catalog.Item(c.Request.Context(), id, "client")
	if err != nil {
		s.CatalogError(c, err)
		return
	}
	itemAssets(row)
	c.JSON(200, gin.H{"data": row})
}
func (s *Handler) npcs(c *gin.Context) {
	p, ok := s.page(c, 32)
	if !ok {
		return
	}
	page, err := s.Catalog.NPCs(c.Request.Context(), catalog.NPCQuery{Search: c.Query("query"), OnMap: c.Query("onMap"), CurrentMap: c.Query("currentMap"), GameOnly: true, Pagination: p})
	if err != nil {
		s.CatalogError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": page.Data, "total": page.Total})
}
func (s *Handler) mapNPCs(c *gin.Context) {
	rows, err := s.Catalog.NPCsOnMap(c.Request.Context(), c.Param("map"), true)
	if err != nil {
		s.CatalogError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows, "total": len(rows)})
}
func (s *Handler) maps(c *gin.Context) {
	p, ok := s.page(c, 35)
	if !ok {
		return
	}
	values, err := s.GameGateway.BattleConfig(c.Request.Context())
	if err != nil {
		s.PlayerError(c, err)
		return
	}
	page, err := s.Maps.Maps(catalog.MapQuery{GameOnly: true, AdventureOrder: true, ChannelsEnabled: values["navigation_map_channels_enabled"] != 0, Search: c.Query("query"), OnMap: c.Query("onMap"), CurrentMap: c.Query("currentMap"), Pagination: p})
	if err != nil {
		s.InternalError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": page.Data, "total": page.Total})
}
