package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/alexedwards/scs/mysqlstore"
	"github.com/alexedwards/scs/v2"
	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"github.com/happyro/happyro-api/internal/audit"
	"github.com/happyro/happyro-api/internal/catalog"
	charactersapi "github.com/happyro/happyro-api/internal/characters"
	"github.com/happyro/happyro-api/internal/config"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
	rewardsapi "github.com/happyro/happyro-api/internal/rewards"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"github.com/happyro/happyro-api/internal/settings"
	"github.com/happyro/happyro-api/internal/transport/http"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func Serve(ctx context.Context, cfg config.Config, db *sqlx.DB) error {
	if err := cfg.ValidateServer(); err != nil {
		return err
	}
	logConfig := zap.NewProductionConfig()
	logConfig.OutputPaths = []string{"stdout"}
	if err := logConfig.Level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return errors.New("invalid LOG_LEVEL")
	}
	logger, err := logConfig.Build()
	if err != nil {
		return err
	}
	defer logger.Sync()
	service, err := sessionauth.NewAdminService(&identityapi.SQLRepository{DB: db}, identityapi.Argon2id{})
	if err != nil {
		return err
	}
	store := mysqlstore.New(db.DB)
	defer store.StopCleanup()
	sessions := scs.New()
	sessions.Store = store
	sessions.Lifetime = cfg.SessionLifetime
	sessions.Cookie.Name = "happyro_admin_session"
	sessions.Cookie.Secure = cfg.SecureCookies
	sessions.Cookie.HttpOnly = true
	sessions.Cookie.SameSite = http.SameSiteLaxMode
	sessions.Cookie.Persist = false
	sessions.ErrorFunc = func(w http.ResponseWriter, _ *http.Request, _ error) {
		logger.Error("session storage failed")
		http.Error(w, "会话存储不可用。", http.StatusServiceUnavailable)
	}
	game, err := database.Open(ctx, cfg.GameDB)
	if err != nil {
		return err
	}
	defer game.Close()
	if err = database.CheckRuntime(ctx, game); err != nil {
		return err
	}
	logDBConfig := cfg.GameDB
	logDBConfig.Name = cfg.GameLogDatabase
	gameLog, err := database.Open(ctx, logDBConfig)
	if err != nil {
		return err
	}
	defer gameLog.Close()
	index, err := os.Open(cfg.MapIndexPath)
	if err != nil {
		return err
	}
	maps, err := catalog.LoadMaps(os.DirFS(cfg.WorldAssetRoot), index)
	index.Close()
	if err != nil {
		return err
	}
	accountService := &accountsapi.Service{Reader: &accountsapi.SQLReader{Game: game, Log: gameLog}}
	characterService := &charactersapi.Service{Reader: &charactersapi.SQLReader{Game: game}, Maps: maps}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: time.Second, KeepAlive: 30 * time.Second}).DialContext
	defer transport.CloseIdleConnections()
	gateway := &gameapi.HTTPGateway{BaseURL: cfg.GameControlURL, Token: cfg.GameControlToken, Client: &http.Client{Timeout: cfg.GameControlTimeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	commands := &gameapi.Service{Commands: &gameapi.SQLRepository{DB: db}, Gateway: gateway, Timeout: cfg.GameControlTimeout}
	snapshots := &gameapi.Snapshots{Gateway: gateway, Characters: characterService}
	aliasesRaw, err := os.ReadFile(filepath.Join(cfg.WorldAssetRoot, "npc-search-aliases.json"))
	if err != nil {
		return err
	}
	var aliases struct {
		Aliases map[string]string `json:"aliases"`
	}
	if err = json.Unmarshal(aliasesRaw, &aliases); err != nil {
		return err
	}
	assets := catalog.FilesystemAssets{ItemRoot: cfg.ItemImageRoot, MonsterRoot: cfg.MonsterImageRoot, WorldRoot: cfg.WorldAssetRoot}
	catalogs := &catalog.Service{Repository: &catalog.SQLRepository{DB: db, Aliases: aliases.Aliases}, Assets: assets}
	records := &rewardsapi.SQLRecords{DB: db}
	grantService := &rewardsapi.Service{Items: catalogs, Records: records, Mail: &rewardsapi.SQLMail{DB: game}, Commands: commands, Audit: &audit.SQLStore{DB: db}}
	deletions := &accountsapi.Deletions{Store: &accountsapi.SQLDeletions{DB: game}, Accounts: &accountsapi.SQLWriter{DB: game}, Audit: &audit.SQLStore{DB: db}}
	revisions := &settings.SQLRevisions{DB: db}
	settingsService := &settings.Service{Revisions: revisions, File: settings.File{Path: cfg.BattleConfigPath}, Commands: commands, Gateway: gateway}
	app := &httptransport.Server{SettingsRecovery: &settings.Recovery{Revisions: revisions, Settings: settingsService, Audit: &audit.SQLStore{DB: db}}, Settings: settingsService, SettingsHistory: revisions, PlayerLimits: &sessionauth.SQLPlayerLimiter{DB: db}, GameAuth: &sessionauth.GameAuthentication{Repository: &sessionauth.SQLGameIdentity{DB: game}, AdminGroups: cfg.GameAdminGroups}, PlayerAccess: &sessionauth.PlayerAccess{Gateway: gateway}, PlayerCharacters: &charactersapi.PlayerService{Access: &sessionauth.PlayerAccess{Gateway: gateway}, Gateway: gateway, Commands: commands}, PlayerRewards: &rewardsapi.PlayerService{Access: &sessionauth.PlayerAccess{Gateway: gateway}, Rewards: grantService}, AccountDeletions: deletions, AccountBatches: &accountsapi.Batches{Deletions: deletions, Records: &accountsapi.SQLBatches{DB: db}, Accounts: &accountsapi.SQLWriter{DB: game}, Commands: commands, Audit: &audit.SQLStore{DB: db}}, AccountManagement: &accountsapi.Management{GM: &accountsapi.SQLWriter{DB: game}, Accounts: &accountsapi.SQLWriter{DB: game}, Audit: &audit.SQLStore{DB: db}}, Grants: grantService, GrantHistory: records, GrantLookup: &rewardsapi.Lookup{Items: catalogs, Targets: &rewardsapi.SQLTargets{DB: game}}, Audit: &audit.SQLStore{DB: db}, Catalog: catalogs, CatalogAssets: assets, GameCommands: commands, Snapshots: snapshots, Maps: maps, GameGateway: gateway, Config: cfg, Auth: service, Passwords: identityapi.NewSelfPasswordService(&identityapi.SQLRepository{DB: db}, identityapi.Argon2id{}), Sessions: sessions, Logger: logger, Accounts: accountService, Characters: characterService}
	handler, err := app.Handler()
	if err != nil {
		return err
	}
	server := &http.Server{Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	recoveryCtx, stopRecovery := context.WithCancel(ctx)
	recoveryDone := make(chan struct{})
	go func() {
		defer close(recoveryDone)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			attempt, cancel := context.WithTimeout(recoveryCtx, 5*time.Second)
			err := settingsService.RecoverPending(attempt)
			cancel()
			if err != nil && recoveryCtx.Err() == nil {
				logger.Warn("automatic settings recovery pending", zap.Error(err))
			}
			select {
			case <-recoveryCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	defer func() { stopRecovery(); <-recoveryDone }()
	stopped := make(chan error, 1)
	go func() { stopped <- server.ListenAndServe() }()
	logger.Info("HTTP server started", zap.String("address", cfg.Listen))
	select {
	case err := <-stopped:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			server.Close()
			return err
		}
		return nil
	}
}
