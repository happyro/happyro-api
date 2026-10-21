package admin

import (
	"github.com/gin-gonic/gin"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
	rewardsapi "github.com/happyro/happyro-api/internal/rewards"
	"github.com/happyro/happyro-api/internal/transport/http/response"
	"strconv"
)

func (s *Handler) grantItems(c *gin.Context) {
	rows, err := s.GrantLookup.FindItems(c.Request.Context(), c.Query("target"))
	if err != nil {
		s.GrantError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows, "success": true})
}
func (s *Handler) grantTargets(c *gin.Context) {
	rows, err := s.GrantLookup.FindTargets(c.Request.Context(), c.Query("target"))
	if err != nil {
		s.GrantError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows, "success": true})
}
func (s *Handler) grantItem(c *gin.Context) {
	var input rewardsapi.ItemRequest
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "物品发放参数格式无效。")
		return
	}
	user := c.MustGet("user").(identityapi.User)
	switch input.Delivery {
	case "", "mail":
		id, err := s.Grants.MailItem(c.Request.Context(), input, user, response.Client(c))
		if err != nil {
			s.GrantError(c, err)
			return
		}
		c.JSON(201, gin.H{"data": gin.H{"mail_id": id}, "success": true})
	case "inventory":
		result, err := s.Grants.Inventory(c.Request.Context(), input, gameapi.Actor{AdminUserID: &user.ID}, response.Client(c))
		if err != nil {
			s.GrantError(c, err)
			return
		}
		c.JSON(201, gin.H{"data": gin.H{"delivery": "inventory", "result": result}, "success": true})
	default:
		s.Fail(c, 422, "validation_failed", "请选择邮件或背包发放。")
	}
}
func (s *Handler) grantZeny(c *gin.Context) {
	var input rewardsapi.ZenyRequest
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "Zeny 发放参数格式无效。")
		return
	}
	user := c.MustGet("user").(identityapi.User)
	result, err := s.Grants.Zeny(c.Request.Context(), input, gameapi.Actor{AdminUserID: &user.ID}, response.Client(c))
	if err != nil {
		s.GrantError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result, "success": true})
}
func (s *Handler) grantHistory(c *gin.Context) {
	page, e1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	p := database.Pagination{Page: page, Size: size}
	if e1 != nil || e2 != nil || !p.Valid() {
		s.Fail(c, 422, "validation_failed", "分页参数无效。")
		return
	}
	history, err := s.GrantHistory.History(c.Request.Context(), p)
	if err != nil {
		s.InternalError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": history.Data, "meta": history.Meta, "success": true})
}
