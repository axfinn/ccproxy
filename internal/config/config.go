package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	JWT     JWTConfig     `mapstructure:"jwt"`
	Claude  ClaudeConfig  `mapstructure:"claude"`
	Admin   AdminConfig   `mapstructure:"admin"`
	Storage StorageConfig `mapstructure:"storage"`
}

type ServerConfig struct {
	Port         int    `mapstructure:"port"`
	Host         string `mapstructure:"host"`
	Mode         string `mapstructure:"mode"` // "web", "api", or "both"
	ReadTimeout  int    `mapstructure:"read_timeout"`
	WriteTimeout int    `mapstructure:"write_timeout"`
}

type JWTConfig struct {
	Secret          string        `mapstructure:"secret"`
	DefaultExpiry   time.Duration `mapstructure:"default_expiry"`
	Issuer          string        `mapstructure:"issuer"`
}

type ClaudeConfig struct {
	APIKeys     []string `mapstructure:"api_keys"`
	APIURL      string   `mapstructure:"api_url"`
	WebURL      string   `mapstructure:"web_url"`
	KeyStrategy string   `mapstructure:"key_strategy"` // "round_robin" or "random"
}

type AdminConfig struct {
	Key string `mapstructure:"key"`
}

type StorageConfig struct {
	DBPath string `mapstructure:"db_path"`
}

var cfg *Config

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	// Set defaults
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.mode", "both")
	viper.SetDefault("server.read_timeout", 30)
	viper.SetDefault("server.write_timeout", 300)

	viper.SetDefault("jwt.default_expiry", "720h")
	viper.SetDefault("jwt.issuer", "ccproxy")

	viper.SetDefault("claude.api_url", "https://api.anthropic.com")
	viper.SetDefault("claude.web_url", "https://claude.ai")
	viper.SetDefault("claude.key_strategy", "round_robin")

	viper.SetDefault("storage.db_path", "./ccproxy.db")

	// Environment variable support
	viper.SetEnvPrefix("CCPROXY")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Read config file if exists
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		// Config file not found, use defaults and env vars
	}

	cfg = &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, err
	}

	// Parse duration for JWT expiry
	if expiryStr := viper.GetString("jwt.default_expiry"); expiryStr != "" {
		if d, err := time.ParseDuration(expiryStr); err == nil {
			cfg.JWT.DefaultExpiry = d
		}
	}

	return cfg, nil
}

func Get() *Config {
	if cfg == nil {
		cfg, _ = Load()
	}
	return cfg
}
