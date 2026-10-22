package httptransport

import (
	"crypto/rand"
	"encoding/hex"
	"github.com/alexedwards/scs/v2"
	"github.com/gin-gonic/gin"
	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"github.com/happyro/happyro-api/internal/audit"
	"github.com/happyro/happyro-api/internal/catalog"
	charactersapi "github.com/happyro/happyro-api/internal/characters"
	"github.com/happyro/happyro-api/internal/config"
	gameapi "github.com/happyro/happyro-api/internal/game"
	rewardsapi "github.com/happyro/happyro-api/internal/rewards"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"github.com/happyro/happyro-api/internal/settings"
	adminhttp "github.com/happyro/happyro-api/internal/transport/http/admin"
	playerhttp "github.com/happyro/happyro-api/internal/transport/http/player"
	"github.com/happyro/happyro-api/internal/transport/http/response"
	"go.uber.org/zap"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Server struct {
	response.Responder
	SettingsRecovery  settings.RecoveryRunner
	Settings          settings.Updater
	SettingsHistory   settings.HistoryReader
	PlayerLimits      sessionauth.PlayerLimiter
	GameAuth          *sessionauth.GameAuthentication
	PlayerAccess      *sessionauth.PlayerAccess
	PlayerCharacters  *charactersapi.PlayerService
	PlayerRewards     *rewardsapi.PlayerService
	AccountDeletions  *accountsapi.Deletions
	AccountBatches    *accountsapi.Batches
	AccountManagement *accountsapi.Management
	Grants            *rewardsapi.Service
	GrantLookup       *rewardsapi.Lookup
	GrantHistory      rewardsapi.RecordHistory
	Audit             audit.HistoryReader
	Config            config.Config
	Catalog           *catalog.Service
	CatalogAssets     catalog.FilesystemAssets
	Auth              *sessionauth.AdminService
	Accounts          *accountsapi.Service
	Characters        *charactersapi.Service
	Maps              *catalog.Maps
	GameGateway       gameapi.Gateway
	GameCommands      *gameapi.Service
	Snapshots         *gameapi.Snapshots
	Sessions          *scs.SessionManager
	Logger            *zap.Logger
}

func (s *Server) Handler() (http.Handler, error) {
	s.Responder = response.Responder{Logger: s.Logger, CatalogAssets: s.CatalogAssets}
	adminModule := &adminhttp.Handler{Responder: response.Responder{Logger: s.Logger, CatalogAssets: s.CatalogAssets}, SettingsRecovery: s.SettingsRecovery, Settings: s.Settings, SettingsHistory: s.SettingsHistory, AccountDeletions: s.AccountDeletions, AccountBatches: s.AccountBatches, AccountManagement: s.AccountManagement, Grants: s.Grants, GrantLookup: s.GrantLookup, GrantHistory: s.GrantHistory, Audit: s.Audit, Config: s.Config, Catalog: s.Catalog, Auth: s.Auth, Accounts: s.Accounts, Characters: s.Characters, Maps: s.Maps, GameGateway: s.GameGateway, GameCommands: s.GameCommands, Snapshots: s.Snapshots, Sessions: s.Sessions}
	playerModule := &playerhttp.Handler{Responder: response.Responder{Logger: s.Logger, CatalogAssets: s.CatalogAssets}, Settings: s.Settings, PlayerLimits: s.PlayerLimits, GameAuth: s.GameAuth, PlayerAccess: s.PlayerAccess, PlayerCharacters: s.PlayerCharacters, PlayerRewards: s.PlayerRewards, Catalog: s.Catalog, Maps: s.Maps, GameGateway: s.GameGateway}
	router := gin.New()
	if err := router.SetTrustedProxies(s.Config.TrustedProxies); err != nil {
		return nil, err
	}
	router.Use(s.observe, s.protect)
	router.GET("/api/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "happyro-api"}) })
	router.GET("/api/public-config", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"timeZone": s.Config.TimeZone})
	})
	playerModule.Register(router)
	// Administrator sessions and CSRF apply only to the administrator API.
	admin := gin.New()
	if err := admin.SetTrustedProxies(s.Config.TrustedProxies); err != nil {
		return nil, err
	}
	admin.Use(adminModule.Protect)
	adminModule.Register(admin)
	adminHandler := gin.WrapH(s.Sessions.LoadAndSave(admin))
	for _, route := range admin.Routes() {
		router.Handle(route.Method, route.Path, adminHandler)
	}
	router.NoRoute(s.frontend)
	return router, nil
}

func (s *Server) observe(c *gin.Context) {
	start := time.Now()
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	requestID := hex.EncodeToString(id)
	c.Header("X-Request-ID", requestID)
	c.Set("request_id", requestID)
	defer func() {
		if recover() != nil {
			s.Logger.Error("request panic", zap.String("request_id", requestID))
			if !c.Writer.Written() {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": "服务器内部错误。", "request_id": requestID})
			}
			c.Abort()
		}
		// Never log the query, request body, cookies, headers or raw error: they may
		// contain game tokens, passwords or SQL arguments.
		s.Logger.Info("http request", zap.String("request_id", requestID), zap.String("method", c.Request.Method), zap.String("route", c.FullPath()), zap.Int("status", c.Writer.Status()), zap.Duration("duration", time.Since(start)))
	}()
	c.Next()
}

func (s *Server) protect(c *gin.Context) {
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "same-origin")
	c.Header("X-Frame-Options", "SAMEORIGIN")
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		c.Header("Cache-Control", "no-store")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	c.Next()
}

func (s *Server) frontend(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/api/") || c.Request.Method != http.MethodGet {
		s.Fail(c, http.StatusNotFound, "not_found", "接口不存在。")
		return
	}
	root, err := os.OpenRoot(s.Config.FrontendDir)
	if err != nil {
		s.Fail(c, http.StatusNotFound, "not_found", "前端资源尚未构建。")
		return
	}
	defer root.Close()
	path := strings.TrimPrefix(c.Request.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	file, err := root.Open(path)
	if err == nil {
		info, statErr := file.Stat()
		if statErr == nil && info.IsDir() {
			file.Close()
			file, err = root.Open(filepath.Join(path, "index.html"))
		}
	}
	if err != nil {
		if filepath.Ext(path) != "" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		file, err = root.Open("index.html")
	}
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	http.ServeContent(c.Writer, c.Request, stat.Name(), stat.ModTime(), file)
}
