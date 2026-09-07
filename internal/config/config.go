package config

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

// Config is the root configuration mapped from config.toml
// SSOT: [server], [security], [database], [admin] blocks must exist.
type Config struct {
	Server   ServerConfig   `toml:"server"`
	Security SecurityConfig `toml:"security"`
	Database DatabaseConfig `toml:"database"`
	Admin    AdminConfig    `toml:"admin"`
}

type ServerConfig struct {
	Host         string `toml:"host"`
	Port         int    `toml:"port"`
	ReadTimeout  string `toml:"read_timeout"`
	WriteTimeout string `toml:"write_timeout"`
	Mode         string `toml:"mode"`
}

type SecurityConfig struct {
	JWTSecret        string `toml:"jwt_secret"`
	JWTAccessExpire  string `toml:"jwt_access_expire"`
	JWTRefreshExpire string `toml:"jwt_refresh_expire"`
	BcryptCost       int    `toml:"bcrypt_cost"`
}

type DatabaseConfig struct {
	Path                   string `toml:"path"`
	MaxOpenConns           int    `toml:"max_open_conns"`
	MaxIdleConns           int    `toml:"max_idle_conns"`
	BusyTimeout            string `toml:"busy_timeout"`
	WalCheckpointTruncate  bool   `toml:"wal_checkpoint_truncate"`
}

type AdminConfig struct {
	Username string `toml:"username"`
	Password string `toml:"password"`
	Email    string `toml:"email"`
}

// Load reads and parses config.toml from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return &cfg, nil
}

// LoadDefault loads config.toml from the repository root or provided path fallback.
func LoadDefault() (*Config, error) {
	candidates := []string{"config.toml", "./config.toml", "../config.toml"}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return Load(p)
		}
	}
	return nil, fmt.Errorf("config.toml not found in candidates %v", candidates)
}

func (c *Config) Validate() error {
	if c.Server.Port == 0 {
		return fmt.Errorf("server.port must be non-zero")
	}
	if c.Security.JWTSecret == "" {
		return fmt.Errorf("security.jwt_secret must not be empty")
	}
	if c.Security.BcryptCost < 4 || c.Security.BcryptCost > 31 {
		c.Security.BcryptCost = 10
	}
	if c.Database.Path == "" {
		return fmt.Errorf("database.path must not be empty")
	}
	if c.Admin.Username == "" {
		return fmt.Errorf("admin.username must not be empty")
	}
	if c.Admin.Password == "" {
		return fmt.Errorf("admin.password must not be empty")
	}
	return nil
}

// Addr returns host:port string.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}
