package admin

import (
	"errors"
	"github.com/gin-gonic/gin"
	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/happyro/happyro-api/internal/transport/http/response"
	"strconv"
)

func (s *Handler) accountError(c *gin.Context, err error) {
	var validation *accountsapi.ValidationError
	switch {
	case errors.As(err, &validation):
		s.Fail(c, 422, validation.Code, validation.Message)
	case errors.Is(err, accountsapi.ErrDeletionWorker):
		s.Fail(c, 503, "deletion_worker_unavailable", err.Error())
	case errors.Is(err, accountsapi.ErrDeletionState):
		s.Fail(c, 409, "deletion_state_conflict", err.Error())
	case errors.Is(err, database.ErrAccountBusy):
		s.Fail(c, 409, "account_busy", err.Error())
	case errors.Is(err, accountsapi.ErrAccountDeleting):
		s.Fail(c, 409, "account_deleting", err.Error())
	case errors.Is(err, accountsapi.ErrAccountExists):
		s.Fail(c, 409, "account_exists", err.Error())
	case errors.Is(err, accountsapi.ErrAccountNotFound):
		s.Fail(c, 404, "account_not_found", err.Error())
	case errors.Is(err, accountsapi.ErrGroupChanged):
		s.Fail(c, 409, "account_group_changed", err.Error())
	case errors.Is(err, accountsapi.ErrGMPermission):
		s.Fail(c, 403, "forbidden", err.Error())
	case errors.Is(err, accountsapi.ErrProtected):
		s.Fail(c, 403, "protected_account", err.Error())
	default:
		s.GameError(c, err)
	}
}
func (s *Handler) accountID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("accountId"), 10, 32)
	if err != nil || id < 1 {
		s.accountError(c, accountsapi.ErrAccountNotFound)
		return 0, false
	}
	return id, true
}
func (s *Handler) createAccount(c *gin.Context) {
	var input accountsapi.NewAccount
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "账号参数格式无效。")
		return
	}
	row, err := s.AccountManagement.Create(c.Request.Context(), input, c.MustGet("user").(identityapi.User), response.Client(c))
	if err != nil {
		s.accountError(c, err)
		return
	}
	c.JSON(201, gin.H{"data": row})
}
func (s *Handler) updateAccount(c *gin.Context) {
	id, ok := s.accountID(c)
	if !ok {
		return
	}
	var input accountsapi.AccountUpdate
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "账号参数格式无效。")
		return
	}
	row, err := s.AccountManagement.Update(c.Request.Context(), id, input, c.MustGet("user").(identityapi.User), response.Client(c))
	if err != nil {
		s.accountError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": row})
}
func (s *Handler) accountPassword(c *gin.Context) {
	id, ok := s.accountID(c)
	if !ok {
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "密码参数格式无效。")
		return
	}
	row, err := s.AccountManagement.Password(c.Request.Context(), id, input.Password, c.MustGet("user").(identityapi.User), response.Client(c))
	if err != nil {
		s.accountError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": row})
}

func (s *Handler) batchAccounts(c *gin.Context) {
	var input accountsapi.BatchInput
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "批量操作参数格式无效。")
		return
	}
	result, err := s.AccountBatches.Run(c.Request.Context(), input, c.MustGet("user").(identityapi.User), response.Client(c))
	if err != nil {
		s.accountError(c, err)
		return
	}
	status := 200
	if result.Status == "accepted" {
		status = 202
	}
	c.JSON(status, result)
}

func (s *Handler) deleteAccount(c *gin.Context) {
	id, ok := s.accountID(c)
	if !ok {
		return
	}
	row, err := s.AccountDeletions.Request(c.Request.Context(), id, c.MustGet("user").(identityapi.User), response.Client(c))
	if err != nil {
		s.accountError(c, err)
		return
	}
	status := 202
	if row.Status == "completed" {
		status = 200
	}
	c.JSON(status, gin.H{"data": row})
}
func (s *Handler) accountDeletion(c *gin.Context) {
	id, ok := s.accountID(c)
	if !ok {
		return
	}
	row, err := s.AccountDeletions.Status(c.Request.Context(), id)
	if err != nil {
		s.accountError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": row})
}
func (s *Handler) retryAccountDeletion(c *gin.Context) {
	id, ok := s.accountID(c)
	if !ok {
		return
	}
	row, err := s.AccountDeletions.Retry(c.Request.Context(), id, c.MustGet("user").(identityapi.User), response.Client(c))
	if err != nil {
		s.accountError(c, err)
		return
	}
	c.JSON(202, gin.H{"data": row})
}

func (s *Handler) accountDeletions(c *gin.Context) {
	page, ok := s.Pagination(c)
	if !ok {
		return
	}
	result, err := s.AccountDeletions.List(c.Request.Context(), page)
	if err != nil {
		s.accountError(c, err)
		return
	}
	c.JSON(200, result)
}

func (s *Handler) setAccountGM(c *gin.Context) {
	id, ok := s.accountID(c)
	if !ok {
		return
	}
	var input accountsapi.GMUpdate
	if err := response.DecodeJSON(c, &input); err != nil {
		s.Fail(c, 422, "validation_failed", "GM 参数格式无效。")
		return
	}
	row, err := s.AccountManagement.SetGM(c.Request.Context(), id, input, c.MustGet("user").(identityapi.User), response.Client(c))
	if err != nil {
		s.accountError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": row})
}
