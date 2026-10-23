// Package config loads immutable, typed process configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/caarlos0/env/v11"
	"github.com/go-sql-driver/mysql"
)

type Database struct {
	Host     string `env:"HOST" envDefault:"127.0.0.1"`
	Port     uint16 `env:"PORT" envDefault:"3306"`
	Name     string `env:"DATABASE"`
	Username string `env:"USERNAME"`
	Password string `env:"PASSWORD"`
}

func (d Database) DSN() string {
	c := mysql.NewConfig()
	c.User, c.Passwd, c.DBName = d.Username, d.Password, d.Name
	c.Net, c.Addr = "tcp", net.JoinHostPort(d.Host, strconv.Itoa(int(d.Port)))
	c.ParseTime, c.Loc = true, time.UTC
	c.Collation = "utf8mb4_unicode_ci"
	c.Timeout, c.ReadTimeout, c.WriteTimeout = 5*time.Second, 15*time.Second, 15*time.Second
	c.Params = map[string]string{"charset": "utf8mb4", "time_zone": "'+00:00'"}
	return c.FormatDSN()
}

type Config struct {
	AdminCSRFOriginCheckEnabled bool          `env:"ADMIN_CSRF_ORIGIN_CHECK_ENABLED" envDefault:"false"`
	TimeZone                    string        `env:"APP_TIMEZONE" envDefault:"Asia/Shanghai"`
	Listen                      string        `env:"HTTP_LISTEN" envDefault:":8080"`
	PublicURL                   string        `env:"APP_URL" envDefault:"http://localhost:8080"`
	FrontendDir                 string        `env:"FRONTEND_DIR" envDefault:"../happyro-admin/dist"`
	LogLevel                    string        `env:"LOG_LEVEL" envDefault:"info"`
	AdminDB                     Database      `envPrefix:"DB_"`
	GameDB                      Database      `envPrefix:"GAME_DB_"`
	GameLogDatabase             string        `env:"GAME_LOG_DB_DATABASE" envDefault:"happyro_log"`
	InitialUsername             string        `env:"ADMIN_INITIAL_USERNAME" envDefault:"admin"`
	InitialPassword             string        `env:"ADMIN_INITIAL_PASSWORD" envDefault:"admin"`
	ShowDefaultCredentials      bool          `env:"SHOW_DEFAULT_CREDENTIALS" envDefault:"false"`
	SessionLifetime             time.Duration `env:"SESSION_LIFETIME" envDefault:"2h"`
	RememberLifetime            time.Duration `env:"SESSION_REMEMBER_LIFETIME" envDefault:"720h"`
	SecureCookies               bool          `env:"SESSION_SECURE_COOKIE" envDefault:"false"`
	TrustedProxies              []string      `env:"HTTP_TRUSTED_PROXIES" envSeparator:","`
	GameControlURL              string        `env:"GAME_CONTROL_BASE_URL" envDefault:"http://127.0.0.1:8889"`
	GameControlToken            string        `env:"GAME_CONTROL_TOKEN"`
	GameControlTimeout          time.Duration `env:"GAME_CONTROL_TIMEOUT" envDefault:"3s"`
	GameAdminGroups             []int         `env:"GAME_ADMIN_GROUP_IDS" envDefault:"2,3,4,10,99" envSeparator:","`
	ItemImageRoot               string        `env:"GAME_ITEM_IMAGE_ROOT"`
	MonsterImageRoot            string        `env:"GAME_MONSTER_IMAGE_ROOT"`
	MapIndexPath                string        `env:"GAME_MAP_INDEX_PATH" envDefault:"../happyro-server/db/map_index.txt"`
	WorldAssetRoot              string        `env:"GAME_WORLD_ASSET_ROOT" envDefault:"resources/game-data/world"`
	BattleConfigPath            string        `env:"GAME_SERVER_BATTLE_CONFIG_PATH"`
}

func Load() (Config, error) {
	c, err := env.ParseAs[Config]()
	if err != nil {
		return c, errors.New("invalid environment configuration (check value types)")
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if c.TimeZone == "" || c.TimeZone == "Local" {
		return errors.New("APP_TIMEZONE must be an explicit IANA time zone")
	}
	if _, err := time.LoadLocation(c.TimeZone); err != nil {
		return errors.New("APP_TIMEZONE must be a valid IANA time zone")
	}
	for name, raw := range map[string]string{"APP_URL": c.PublicURL, "GAME_CONTROL_BASE_URL": c.GameControlURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%s must be an absolute HTTP(S) URL without credentials, query or fragment", name)
		}
	}
	if c.SessionLifetime <= 0 || c.RememberLifetime < c.SessionLifetime || c.GameControlTimeout <= 0 {
		return errors.New("invalid session or game-control duration")
	}
	return nil
}

func (c Config) DefaultCredentialHint() bool {
	return c.ShowDefaultCredentials && c.InitialUsername == "admin" && c.InitialPassword == "admin"
}

// ValidateServer checks requirements of the HTTP service only. Database and
// account administration commands do not publish game configuration files.
func (c Config) ValidateServer() error {
	if strings.TrimSpace(c.BattleConfigPath) == "" || !filepath.IsAbs(c.BattleConfigPath) {
		return errors.New("GAME_SERVER_BATTLE_CONFIG_PATH must be an absolute configuration file path")
	}
	return nil
}
