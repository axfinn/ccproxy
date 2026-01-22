package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server      ServerConfig      `mapstructure:"server"`
	JWT         JWTConfig         `mapstructure:"jwt"`
	Claude      ClaudeConfig      `mapstructure:"claude"`
	Admin       AdminConfig       `mapstructure:"admin"`
	Storage     StorageConfig     `mapstructure:"storage"`
	Pool        PoolConfig        `mapstructure:"pool"`
	Circuit     CircuitConfig     `mapstructure:"circuit"`
	Concurrency ConcurrencyConfig `mapstructure:"concurrency"`
	RateLimit   RateLimitConfig   `mapstructure:"ratelimit"`
	Retry       RetryConfig       `mapstructure:"retry"`
	Health      HealthConfig      `mapstructure:"health"`
	Scheduler   SchedulerConfig   `mapstructure:"scheduler"`
	Metrics     MetricsConfig     `mapstructure:"metrics"`
}

type ServerConfig struct {
	Port         int    `mapstructure:"port"`
	Host         string `mapstructure:"host"`
	Mode         string `mapstructure:"mode"` // "web", "api", or "both"
	ReadTimeout  int    `mapstructure:"read_timeout"`
	WriteTimeout int    `mapstructure:"write_timeout"`
}

type JWTConfig struct {
	Secret        string        `mapstructure:"secret"`
	DefaultExpiry time.Duration `mapstructure:"default_expiry"`
	Issuer        string        `mapstructure:"issuer"`
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

// PoolConfig holds connection pool configuration
type PoolConfig struct {
	MaxIdleConns        int           `mapstructure:"max_idle_conns"`
	MaxIdleConnsPerHost int           `mapstructure:"max_idle_conns_per_host"`
	IdleConnTimeout     time.Duration `mapstructure:"idle_conn_timeout"`
	MaxClients          int           `mapstructure:"max_clients"`
	ClientIdleTTL       time.Duration `mapstructure:"client_idle_ttl"`
	ResponseTimeout     time.Duration `mapstructure:"response_timeout"`
}

// CircuitConfig holds circuit breaker configuration
type CircuitConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	FailureThreshold int           `mapstructure:"failure_threshold"`
	SuccessThreshold int           `mapstructure:"success_threshold"`
	OpenTimeout      time.Duration `mapstructure:"open_timeout"`
}

// ConcurrencyConfig holds concurrency control configuration
type ConcurrencyConfig struct {
	UserMax       int           `mapstructure:"user_max"`
	AccountMax    int           `mapstructure:"account_max"`
	MaxWaitQueue  int           `mapstructure:"max_wait_queue"`
	WaitTimeout   time.Duration `mapstructure:"wait_timeout"`
	BackoffBase   time.Duration `mapstructure:"backoff_base"`
	BackoffMax    time.Duration `mapstructure:"backoff_max"`
	BackoffJitter float64       `mapstructure:"backoff_jitter"`
	PingInterval  time.Duration `mapstructure:"ping_interval"`
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	Enabled      bool      `mapstructure:"enabled"`
	UserLimit    LimitRule `mapstructure:"user_limit"`
	AccountLimit LimitRule `mapstructure:"account_limit"`
	IPLimit      LimitRule `mapstructure:"ip_limit"`
	GlobalLimit  LimitRule `mapstructure:"global_limit"`
}

// LimitRule defines a rate limit rule
type LimitRule struct {
	Requests int           `mapstructure:"requests"`
	Window   time.Duration `mapstructure:"window"`
}

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxAttempts        int           `mapstructure:"max_attempts"`
	MaxAccountSwitches int           `mapstructure:"max_account_switches"`
	InitialBackoff     time.Duration `mapstructure:"initial_backoff"`
	MaxBackoff         time.Duration `mapstructure:"max_backoff"`
	Jitter             float64       `mapstructure:"jitter"`
}

// HealthConfig holds health monitor configuration
type HealthConfig struct {
	Enabled            bool          `mapstructure:"enabled"`
	CheckInterval      time.Duration `mapstructure:"check_interval"`
	TokenRefreshBefore time.Duration `mapstructure:"token_refresh_before"`
	Timeout            time.Duration `mapstructure:"timeout"`
}

// SchedulerConfig holds scheduler configuration
type SchedulerConfig struct {
	StickySessionTTL time.Duration `mapstructure:"sticky_session_ttl"`
	Strategy         string        `mapstructure:"strategy"` // "least_loaded", "round_robin", "random"
}

// MetricsConfig holds Prometheus metrics configuration
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
}

var cfg *Config

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	// Set defaults - Server
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.mode", "both")
	viper.SetDefault("server.read_timeout", 30)
	viper.SetDefault("server.write_timeout", 300)

	// Set defaults - JWT
	viper.SetDefault("jwt.default_expiry", "720h")
	viper.SetDefault("jwt.issuer", "ccproxy")

	// Set defaults - Claude
	viper.SetDefault("claude.api_url", "https://api.anthropic.com")
	viper.SetDefault("claude.web_url", "https://claude.ai")
	viper.SetDefault("claude.key_strategy", "round_robin")

	// Set defaults - Storage
	viper.SetDefault("storage.db_path", "./ccproxy.db")

	// Set defaults - Pool
	viper.SetDefault("pool.max_idle_conns", 240)
	viper.SetDefault("pool.max_idle_conns_per_host", 120)
	viper.SetDefault("pool.idle_conn_timeout", "90s")
	viper.SetDefault("pool.max_clients", 5000)
	viper.SetDefault("pool.client_idle_ttl", "15m")
	viper.SetDefault("pool.response_timeout", "10m")

	// Set defaults - Circuit Breaker
	viper.SetDefault("circuit.enabled", true)
	viper.SetDefault("circuit.failure_threshold", 5)
	viper.SetDefault("circuit.success_threshold", 2)
	viper.SetDefault("circuit.open_timeout", "30s")

	// Set defaults - Concurrency
	viper.SetDefault("concurrency.user_max", 10)
	viper.SetDefault("concurrency.account_max", 5)
	viper.SetDefault("concurrency.max_wait_queue", 20)
	viper.SetDefault("concurrency.wait_timeout", "30s")
	viper.SetDefault("concurrency.backoff_base", "100ms")
	viper.SetDefault("concurrency.backoff_max", "2s")
	viper.SetDefault("concurrency.backoff_jitter", 0.2)
	viper.SetDefault("concurrency.ping_interval", "5s")

	// Set defaults - Rate Limit
	viper.SetDefault("ratelimit.enabled", true)
	viper.SetDefault("ratelimit.user_limit.requests", 100)
	viper.SetDefault("ratelimit.user_limit.window", "1m")
	viper.SetDefault("ratelimit.account_limit.requests", 1000)
	viper.SetDefault("ratelimit.account_limit.window", "1m")
	viper.SetDefault("ratelimit.ip_limit.requests", 200)
	viper.SetDefault("ratelimit.ip_limit.window", "1m")
	viper.SetDefault("ratelimit.global_limit.requests", 10000)
	viper.SetDefault("ratelimit.global_limit.window", "1m")

	// Set defaults - Retry
	viper.SetDefault("retry.max_attempts", 3)
	viper.SetDefault("retry.max_account_switches", 10)
	viper.SetDefault("retry.initial_backoff", "100ms")
	viper.SetDefault("retry.max_backoff", "2s")
	viper.SetDefault("retry.jitter", 0.2)

	// Set defaults - Health
	viper.SetDefault("health.enabled", true)
	viper.SetDefault("health.check_interval", "5m")
	viper.SetDefault("health.token_refresh_before", "30m")
	viper.SetDefault("health.timeout", "30s")

	// Set defaults - Scheduler
	viper.SetDefault("scheduler.sticky_session_ttl", "1h")
	viper.SetDefault("scheduler.strategy", "least_loaded")

	// Set defaults - Metrics
	viper.SetDefault("metrics.enabled", true)
	viper.SetDefault("metrics.path", "/metrics")

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

	// Parse durations
	parseDurations(cfg)

	return cfg, nil
}

// parseDurations parses duration strings from viper
func parseDurations(cfg *Config) {
	// JWT expiry
	if expiryStr := viper.GetString("jwt.default_expiry"); expiryStr != "" {
		if d, err := time.ParseDuration(expiryStr); err == nil {
			cfg.JWT.DefaultExpiry = d
		}
	}

	// Pool durations
	if d, err := time.ParseDuration(viper.GetString("pool.idle_conn_timeout")); err == nil {
		cfg.Pool.IdleConnTimeout = d
	}
	if d, err := time.ParseDuration(viper.GetString("pool.client_idle_ttl")); err == nil {
		cfg.Pool.ClientIdleTTL = d
	}
	if d, err := time.ParseDuration(viper.GetString("pool.response_timeout")); err == nil {
		cfg.Pool.ResponseTimeout = d
	}

	// Circuit durations
	if d, err := time.ParseDuration(viper.GetString("circuit.open_timeout")); err == nil {
		cfg.Circuit.OpenTimeout = d
	}

	// Concurrency durations
	if d, err := time.ParseDuration(viper.GetString("concurrency.wait_timeout")); err == nil {
		cfg.Concurrency.WaitTimeout = d
	}
	if d, err := time.ParseDuration(viper.GetString("concurrency.backoff_base")); err == nil {
		cfg.Concurrency.BackoffBase = d
	}
	if d, err := time.ParseDuration(viper.GetString("concurrency.backoff_max")); err == nil {
		cfg.Concurrency.BackoffMax = d
	}
	if d, err := time.ParseDuration(viper.GetString("concurrency.ping_interval")); err == nil {
		cfg.Concurrency.PingInterval = d
	}

	// Rate limit durations
	if d, err := time.ParseDuration(viper.GetString("ratelimit.user_limit.window")); err == nil {
		cfg.RateLimit.UserLimit.Window = d
	}
	if d, err := time.ParseDuration(viper.GetString("ratelimit.account_limit.window")); err == nil {
		cfg.RateLimit.AccountLimit.Window = d
	}
	if d, err := time.ParseDuration(viper.GetString("ratelimit.ip_limit.window")); err == nil {
		cfg.RateLimit.IPLimit.Window = d
	}
	if d, err := time.ParseDuration(viper.GetString("ratelimit.global_limit.window")); err == nil {
		cfg.RateLimit.GlobalLimit.Window = d
	}

	// Retry durations
	if d, err := time.ParseDuration(viper.GetString("retry.initial_backoff")); err == nil {
		cfg.Retry.InitialBackoff = d
	}
	if d, err := time.ParseDuration(viper.GetString("retry.max_backoff")); err == nil {
		cfg.Retry.MaxBackoff = d
	}

	// Health durations
	if d, err := time.ParseDuration(viper.GetString("health.check_interval")); err == nil {
		cfg.Health.CheckInterval = d
	}
	if d, err := time.ParseDuration(viper.GetString("health.token_refresh_before")); err == nil {
		cfg.Health.TokenRefreshBefore = d
	}
	if d, err := time.ParseDuration(viper.GetString("health.timeout")); err == nil {
		cfg.Health.Timeout = d
	}

	// Scheduler durations
	if d, err := time.ParseDuration(viper.GetString("scheduler.sticky_session_ttl")); err == nil {
		cfg.Scheduler.StickySessionTTL = d
	}
}

func Get() *Config {
	if cfg == nil {
		cfg, _ = Load()
	}
	return cfg
}
