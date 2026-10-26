// Package config loads immutable, typed process configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
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
	Name     string `env:"NAME"`
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
	RegistrationDefaultGroupID       *int64        `env:"HAPPYRO_REGISTRATION_DEFAULT_GROUP_ID"`
	RegistrationDefaultGroupEditable bool          `env:"HAPPYRO_REGISTRATION_DEFAULT_GROUP_EDITABLE" envDefault:"true"`
	AdminPasswordChangeEnabled       bool          `env:"HAPPYRO_ADMIN_PASSWORD_CHANGE_ENABLED" envDefault:"true"`
	AdminCSRFOriginCheckEnabled      bool          `env:"HAPPYRO_ADMIN_CSRF_ORIGIN_CHECK_ENABLED" envDefault:"false"`
	TimeZone                         string        `env:"HAPPYRO_TIMEZONE" envDefault:"Asia/Shanghai"`
	Listen                           string        `env:"HAPPYRO_API_LISTEN_ADDRESS" envDefault:":8080"`
	PublicURL                        string        `env:"HAPPYRO_ADMIN_PUBLIC_URL" envDefault:"http://localhost:8080"`
	LogLevel                         string        `env:"HAPPYRO_API_LOG_LEVEL" envDefault:"info"`
	AdminDB                          Database      `envPrefix:"HAPPYRO_API_ADMIN_DB_"`
	GameDB                           Database      `envPrefix:"HAPPYRO_API_GAME_DB_"`
	GameLogDatabase                  string        `env:"HAPPYRO_API_GAME_LOG_DB_NAME" envDefault:"happyro_log"`
	InitialUsername                  string        `env:"HAPPYRO_ADMIN_INITIAL_USERNAME" envDefault:"admin"`
	InitialPassword                  string        `env:"HAPPYRO_ADMIN_INITIAL_PASSWORD" envDefault:"admin"`
	ShowDefaultCredentials           bool          `env:"HAPPYRO_ADMIN_DEFAULT_CREDENTIALS_HINT_ENABLED" envDefault:"false"`
	SessionLifetime                  time.Duration `env:"HAPPYRO_ADMIN_SESSION_LIFETIME" envDefault:"2h"`
	RememberLifetime                 time.Duration `env:"HAPPYRO_ADMIN_SESSION_REMEMBER_LIFETIME" envDefault:"720h"`
	SecureCookies                    bool          `env:"HAPPYRO_ADMIN_SESSION_SECURE_COOKIE_ENABLED" envDefault:"false"`
	TrustedProxies                   []string      `env:"HAPPYRO_API_TRUSTED_PROXIES" envSeparator:","`
	GameControlURL                   string        `env:"HAPPYRO_API_GAME_CONTROL_UPSTREAM_URL" envDefault:"http://127.0.0.1:8889"`
	GameControlToken                 string        `env:"HAPPYRO_GAME_CONTROL_TOKEN"`
	GameControlTimeout               time.Duration `env:"HAPPYRO_API_GAME_CONTROL_TIMEOUT" envDefault:"3s"`
	GameAdminGroups                  []int         `env:"HAPPYRO_GAME_ADMIN_GROUP_IDS" envDefault:"2,3,4,10,99" envSeparator:","`
	ItemImageRoot                    string        `env:"HAPPYRO_API_ITEM_IMAGE_DIR"`
	MonsterImageRoot                 string        `env:"HAPPYRO_API_MONSTER_IMAGE_DIR"`
	MapIndexPath                     string        `env:"HAPPYRO_API_MAP_INDEX_PATH" envDefault:"../happyro-server/db/map_index.txt"`
	WorldAssetRoot                   string        `env:"HAPPYRO_API_WORLD_ASSET_DIR" envDefault:"resources/game-data/world"`
	BattleConfigPath                 string        `env:"HAPPYRO_API_BATTLE_CONFIG_PATH"`
}

func Load() (Config, error) {
	// Keep boolean spelling consistent with the Gateway and game deployment API.
	// Empty values retain the field default; every nonempty typo is an error.
	fields := reflect.TypeOf(Config{})
	for i := 0; i < fields.NumField(); i++ {
		field := fields.Field(i)
		if field.Type.Kind() != reflect.Bool {
			continue
		}
		name := field.Tag.Get("env")
		if value := os.Getenv(name); value != "" && value != "true" && value != "false" {
			return Config{}, fmt.Errorf("%s must be true or false", name)
		}
	}

	c, err := env.ParseAs[Config]()
	if err != nil {
		return c, errors.New("invalid environment configuration (check value types)")
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if c.RegistrationDefaultGroupID != nil && *c.RegistrationDefaultGroupID != 0 && *c.RegistrationDefaultGroupID != 99 {
		return errors.New("HAPPYRO_REGISTRATION_DEFAULT_GROUP_ID must be 0 or 99")
	}
	if c.TimeZone == "" || c.TimeZone == "Local" {
		return errors.New("HAPPYRO_TIMEZONE must be an explicit IANA time zone")
	}
	if _, err := time.LoadLocation(c.TimeZone); err != nil {
		return errors.New("HAPPYRO_TIMEZONE must be a valid IANA time zone")
	}
	for name, raw := range map[string]string{"HAPPYRO_ADMIN_PUBLIC_URL": c.PublicURL, "HAPPYRO_API_GAME_CONTROL_UPSTREAM_URL": c.GameControlURL} {
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
		return errors.New("HAPPYRO_API_BATTLE_CONFIG_PATH must be an absolute configuration file path")
	}
	return nil
}
