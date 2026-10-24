package response

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/happyro/happyro-api/internal/catalog"
	charactersapi "github.com/happyro/happyro-api/internal/characters"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
	rewardsapi "github.com/happyro/happyro-api/internal/rewards"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"github.com/happyro/happyro-api/internal/settings"
	"go.uber.org/zap"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type Responder struct {
	Logger        *zap.Logger
	CatalogAssets catalog.FilesystemAssets
}

func (s *Responder) Fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"message": message, "error": gin.H{"code": code, "message": message}, "request_id": c.GetString("request_id")})
}

func (s *Responder) InternalError(c *gin.Context, err error) {
	// The client receives a correlation ID; exception strings may contain secrets.
	s.Logger.Error("request failed", zap.String("request_id", c.GetString("request_id")), zap.String("error_type", ErrorType(err)))
	s.Fail(c, http.StatusInternalServerError, "internal_error", "服务器内部错误。")
}

func ErrorType(err error) string { return fmt.Sprintf("%T", err) }

func Client(c *gin.Context) identityapi.Client {
	return identityapi.Client{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()}
}

func DecodeJSON(c *gin.Context, target any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("body must contain exactly one JSON value")
	}
	return nil
}

func (s *Responder) GameError(c *gin.Context, err error) {
	var validation *gameapi.ValidationError
	var failure *gameapi.GatewayError
	switch {
	case errors.As(err, &validation):
		s.Fail(c, 422, "validation_failed", validation.Message)
	case errors.Is(err, gameapi.ErrConflict):
		s.Fail(c, 409, "idempotency_conflict", err.Error())
	case errors.Is(err, gameapi.ErrRunning):
		s.Fail(c, 409, "command_not_replayable", err.Error())
	case errors.Is(err, gameapi.ErrNotFound), errors.Is(err, charactersapi.ErrNotFound):
		s.Fail(c, 404, "not_found", err.Error())
	case errors.As(err, &failure):
		status := http.StatusBadGateway
		if failure.Code == "account_deleting" || failure.Code == "character_offline" || failure.Code == "no_spawn_cell" || strings.HasPrefix(failure.Code, "navigation_") {
			status = http.StatusConflict
		}
		s.Fail(c, status, failure.Code, gameapi.Message(failure.Code, failure.Message))
	default:
		s.InternalError(c, err)
	}
}

func (s *Responder) CatalogError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		s.Fail(c, 404, "not_found", err.Error())
	case errors.Is(err, catalog.ErrNotReady):
		s.Fail(c, 503, "catalog_unavailable", err.Error())
	case errors.Is(err, catalog.ErrQuery):
		s.Fail(c, 422, "validation_failed", err.Error())
	default:
		s.InternalError(c, err)
	}
}

func (s *Responder) CatalogID(c *gin.Context, param string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(param), 10, 32)
	if err != nil || id <= 0 {
		s.Fail(c, 404, "not_found", "资料不存在。")
		return 0, false
	}
	return id, true
}

func (s *Responder) CatalogAsset(kind, param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := s.CatalogID(c, param)
		if !ok {
			return
		}
		directory, path := s.CatalogAssets.Location(kind, id)
		s.Asset(c, directory, path)
	}
}

func (s *Responder) GrantError(c *gin.Context, err error) {
	var validation *rewardsapi.ValidationError
	var gateway *gameapi.GatewayError
	switch {
	case errors.As(err, &validation):
		s.Fail(c, 422, "validation_failed", validation.Message)
	case errors.Is(err, rewardsapi.ErrConflict):
		s.Fail(c, 409, "idempotency_conflict", err.Error())
	case errors.Is(err, rewardsapi.ErrPending):
		s.Fail(c, 409, "delivery_pending", err.Error())
	case errors.Is(err, rewardsapi.ErrAccountDeleting):
		s.Fail(c, 409, "account_deleting", err.Error())
	case errors.Is(err, database.ErrAccountBusy):
		s.Fail(c, 409, "account_busy", err.Error())
	case errors.Is(err, rewardsapi.ErrCharacter):
		s.Fail(c, 404, "character_not_found", err.Error())
	case errors.Is(err, catalog.ErrNotFound):
		s.Fail(c, 422, "item_not_found", "物品不存在或不在服务端资料中。")
	case errors.Is(err, catalog.ErrNotReady), errors.Is(err, catalog.ErrQuery):
		s.CatalogError(c, err)
	case errors.As(err, &gateway):
		status := 502
		switch gateway.Code {
		case "invalid_parameter", "item_not_found", "item_not_grantable":
			status = 422
		case "account_deleting", "character_offline", "inventory_full", "inventory_overweight", "item_amount_exceeded", "command_not_replayable", "insufficient_zeny":
			status = 409
		}
		s.Fail(c, status, gateway.Code, gameapi.Message(gateway.Code, gateway.Message))
	default:
		s.GameError(c, err)
	}
}

func (s *Responder) Pagination(c *gin.Context) (database.Pagination, bool) {
	return s.PaginationWithSize(c, "perPage")
}

func (s *Responder) PaginationWithSize(c *gin.Context, sizeKey string) (database.Pagination, bool) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		s.Fail(c, 422, "validation_failed", "页码必须是正整数。")
		return database.Pagination{}, false
	}
	size, err := strconv.Atoi(c.DefaultQuery(sizeKey, "20"))
	if err != nil {
		s.Fail(c, 422, "validation_failed", "每页数量必须是 1 至 100 的整数。")
		return database.Pagination{}, false
	}
	p := database.Pagination{Page: page, Size: size}
	if !p.Valid() {
		s.Fail(c, 422, "validation_failed", "页码或每页数量超出范围。")
		return p, false
	}
	return p, true
}

func (s *Responder) Asset(c *gin.Context, directory, path string) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		c.AbortWithStatus(404)
		return
	}
	defer root.Close()
	file, err := root.Open(path)
	if err != nil {
		c.AbortWithStatus(404)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		c.AbortWithStatus(404)
		return
	}
	if strings.HasPrefix(c.Request.URL.Path, "/api/adventure-tools/") {
		c.Header("Cache-Control", "private, max-age=86400")
	} else {
		c.Header("Cache-Control", "public, max-age=86400")
	}
	http.ServeContent(c.Writer, c.Request, stat.Name(), stat.ModTime(), file)
}

func (s *Responder) PlayerError(c *gin.Context, err error) {
	if errors.Is(err, sessionauth.ErrPlayerForbidden) {
		s.Fail(c, 403, "forbidden", err.Error())
		return
	}
	s.GameError(c, err)
}

func (s *Responder) SettingError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, settings.ErrAutoRestored):
		s.Fail(c, 409, "settings_auto_restored", err.Error())
	case errors.Is(err, settings.ErrRevisionNotFound):
		s.Fail(c, 404, "revision_not_found", err.Error())
	case errors.Is(err, settings.ErrBusy):
		s.Fail(c, 409, "settings_busy", err.Error())
	case errors.Is(err, settings.ErrUnconfirmed):
		s.Fail(c, 409, "settings_unconfirmed", err.Error())
	case errors.Is(err, settings.ErrFileChanged):
		s.Fail(c, 409, "settings_file_changed", err.Error())
	default:
		s.PlayerError(c, err)
	}
}
