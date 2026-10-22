package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/happyro/happyro-api/internal/catalog"
)

func (s *Handler) items(c *gin.Context) {
	p, ok := s.Pagination(c)
	if !ok {
		return
	}
	page, err := s.Catalog.Items(c.Request.Context(), catalog.ItemQuery{Search: c.Query("query"), Type: c.Query("type"), Subtype: c.Query("subtype"), Range: c.DefaultQuery("range", "client"), DropKind: c.DefaultQuery("dropKind", "all"), Pagination: p})
	if err != nil {
		s.CatalogError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": page.Data, "total": page.Total, "subtypes": page.Subtypes, "success": true})
}
func (s *Handler) item(c *gin.Context) {
	id, ok := s.CatalogID(c, "itemId")
	if !ok {
		return
	}
	row, err := s.Catalog.Item(c.Request.Context(), id, c.DefaultQuery("range", "client"))
	if err != nil {
		s.CatalogError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": row, "success": true})
}
func (s *Handler) monsters(c *gin.Context) {
	p, ok := s.Pagination(c)
	if !ok {
		return
	}
	page, err := s.Catalog.Monsters(c.Request.Context(), catalog.MonsterQuery{Search: c.Query("query"), Race: c.Query("race"), Element: c.Query("element"), Size: c.Query("size"), Kind: c.Query("kind"), Pagination: p})
	if err != nil {
		s.CatalogError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": page.Data, "total": page.Total, "success": true})
}
func (s *Handler) monster(c *gin.Context) {
	id, ok := s.CatalogID(c, "monsterId")
	if !ok {
		return
	}
	row, err := s.Catalog.Monster(c.Request.Context(), id)
	if err != nil {
		s.CatalogError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": row, "success": true})
}
func (s *Handler) npcVisibility(c *gin.Context) (bool, bool) {
	visibility := c.DefaultQuery("visibility", "game")
	if visibility != "game" && visibility != "all" {
		s.Fail(c, 422, "validation_failed", "NPC 可见范围无效。")
		return false, false
	}
	return visibility == "game", true
}
func (s *Handler) npcs(c *gin.Context) {
	p, ok := s.Pagination(c)
	if !ok {
		return
	}
	gameOnly, ok := s.npcVisibility(c)
	if !ok {
		return
	}
	page, err := s.Catalog.NPCs(c.Request.Context(), catalog.NPCQuery{Search: c.Query("query"), Map: c.Query("map"), Name: c.Query("name"), DisplayName: c.Query("name_zh_cn"), GameOnly: gameOnly, Pagination: p})
	if err != nil {
		s.CatalogError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": page.Data, "total": page.Total, "success": true})
}
func (s *Handler) mapNPCs(c *gin.Context) {
	gameOnly, ok := s.npcVisibility(c)
	if !ok {
		return
	}
	rows, err := s.Catalog.NPCsOnMap(c.Request.Context(), c.Param("map"), gameOnly)
	if err != nil {
		s.CatalogError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows, "total": len(rows), "success": true})
}
func (s *Handler) catalogVersions(c *gin.Context) {
	versions, err := s.Catalog.Repository.Versions(c.Request.Context())
	if err != nil {
		s.CatalogError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": versions, "success": true})
}

func (s *Handler) catalogHistory(c *gin.Context) {
	p, ok := s.PaginationWithSize(c, "per_page")
	if !ok {
		return
	}
	history, err := s.Audit.History(c.Request.Context(), "settings.game_data_updated", p)
	if err != nil {
		s.InternalError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": history.Data, "meta": history.Meta, "success": true})
}
