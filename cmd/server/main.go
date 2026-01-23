package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"ccproxy/internal/circuit"
	"ccproxy/internal/concurrency"
	"ccproxy/internal/config"
	"ccproxy/internal/handler"
	"ccproxy/internal/health"
	"ccproxy/internal/loadbalancer"
	"ccproxy/internal/metrics"
	"ccproxy/internal/middleware"
	"ccproxy/internal/pool"
	"ccproxy/internal/ratelimit"
	"ccproxy/internal/retry"
	"ccproxy/internal/scheduler"
	"ccproxy/internal/service"
	"ccproxy/internal/store"
	"ccproxy/pkg/jwt"
	"ccproxy/web"
)

func main() {
	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(zerolog.DebugLevel) // Enable debug logging

	// Create log file
	logFile, err := os.OpenFile("ccproxy.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open log file")
	}
	defer logFile.Close()

	// Multi-writer: write to both console and file
	multi := zerolog.MultiLevelWriter(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"},
		logFile,
	)
	log.Logger = log.Output(multi)

	// Load configuration
	cfg, err2 := config.Load()
	if err2 != nil {
		log.Fatal().Err(err2).Msg("failed to load configuration")
	}

	// Validate required configuration
	if cfg.JWT.Secret == "" {
		log.Fatal().Msg("JWT secret is required (set CCPROXY_JWT_SECRET)")
	}
	if cfg.Admin.Key == "" {
		log.Fatal().Msg("Admin key is required (set CCPROXY_ADMIN_KEY)")
	}

	// Initialize store
	db, err := store.New(cfg.Storage.DBPath)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize database")
	}
	defer db.Close()

	// Initialize JWT manager
	jwtManager := jwt.NewManager(cfg.JWT.Secret, cfg.JWT.Issuer)

	// Initialize key pool
	var keyPool *loadbalancer.KeyPool
	if len(cfg.Claude.APIKeys) > 0 {
		keyPool = loadbalancer.NewKeyPool(cfg.Claude.APIKeys, loadbalancer.Strategy(cfg.Claude.KeyStrategy))
		log.Info().Int("keys", keyPool.Size()).Msg("initialized API key pool")
	} else {
		keyPool = loadbalancer.NewKeyPool([]string{}, loadbalancer.StrategyRoundRobin)
		log.Warn().Msg("no API keys configured, API mode will be unavailable")
	}

	// Initialize OAuth service
	oauthService := service.NewOAuthService(cfg.Claude.WebURL, cfg.Claude.APIURL, db)

	// Initialize enhanced components
	httpPool := pool.NewHTTPPool(pool.PoolConfig{
		MaxIdleConns:        cfg.Pool.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.Pool.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.Pool.IdleConnTimeout,
		MaxClients:          cfg.Pool.MaxClients,
		ClientIdleTTL:       cfg.Pool.ClientIdleTTL,
		ResponseTimeout:     cfg.Pool.ResponseTimeout,
	})
	defer httpPool.Close()
	log.Info().Msg("initialized connection pool")

	circuitMgr := circuit.NewManager(circuit.BreakerConfig{
		Enabled:          cfg.Circuit.Enabled,
		FailureThreshold: cfg.Circuit.FailureThreshold,
		SuccessThreshold: cfg.Circuit.SuccessThreshold,
		OpenTimeout:      cfg.Circuit.OpenTimeout,
	})
	defer circuitMgr.Close()
	log.Info().Bool("enabled", cfg.Circuit.Enabled).Msg("initialized circuit breaker manager")

	concurrencyMgr := concurrency.NewManager(concurrency.ConcurrencyConfig{
		UserMax:       cfg.Concurrency.UserMax,
		AccountMax:    cfg.Concurrency.AccountMax,
		MaxWaitQueue:  cfg.Concurrency.MaxWaitQueue,
		WaitTimeout:   cfg.Concurrency.WaitTimeout,
		BackoffBase:   cfg.Concurrency.BackoffBase,
		BackoffMax:    cfg.Concurrency.BackoffMax,
		BackoffJitter: cfg.Concurrency.BackoffJitter,
		PingInterval:  cfg.Concurrency.PingInterval,
	})
	defer concurrencyMgr.Close()
	log.Info().Int("user_max", cfg.Concurrency.UserMax).Int("account_max", cfg.Concurrency.AccountMax).Msg("initialized concurrency manager")

	rateLimiter := ratelimit.NewMultiMemoryLimiter(ratelimit.RateLimitConfig{
		Enabled: cfg.RateLimit.Enabled,
		UserLimit: ratelimit.LimitRule{
			Requests: cfg.RateLimit.UserLimit.Requests,
			Window:   cfg.RateLimit.UserLimit.Window,
		},
		AccountLimit: ratelimit.LimitRule{
			Requests: cfg.RateLimit.AccountLimit.Requests,
			Window:   cfg.RateLimit.AccountLimit.Window,
		},
		IPLimit: ratelimit.LimitRule{
			Requests: cfg.RateLimit.IPLimit.Requests,
			Window:   cfg.RateLimit.IPLimit.Window,
		},
		GlobalLimit: ratelimit.LimitRule{
			Requests: cfg.RateLimit.GlobalLimit.Requests,
			Window:   cfg.RateLimit.GlobalLimit.Window,
		},
	})
	defer rateLimiter.Close()
	log.Info().Bool("enabled", cfg.RateLimit.Enabled).Msg("initialized rate limiter")

	schedulerSvc := scheduler.NewScheduler(scheduler.SchedulerConfig{
		StickySessionTTL: cfg.Scheduler.StickySessionTTL,
		Strategy:         scheduler.Strategy(cfg.Scheduler.Strategy),
	}, circuitMgr, concurrencyMgr)
	defer schedulerSvc.Close()
	log.Info().Str("strategy", cfg.Scheduler.Strategy).Msg("initialized scheduler")

	retryPolicy := retry.NewPolicy(retry.RetryConfig{
		MaxAttempts:        cfg.Retry.MaxAttempts,
		MaxAccountSwitches: cfg.Retry.MaxAccountSwitches,
		InitialBackoff:     cfg.Retry.InitialBackoff,
		MaxBackoff:         cfg.Retry.MaxBackoff,
		Jitter:             cfg.Retry.Jitter,
	})
	retryExecutor := retry.NewExecutor(retryPolicy)
	log.Info().Int("max_attempts", cfg.Retry.MaxAttempts).Int("max_switches", cfg.Retry.MaxAccountSwitches).Msg("initialized retry executor")

	var metricsCollector *metrics.Metrics
	if cfg.Metrics.Enabled {
		metricsCollector = metrics.NewMetrics(metrics.MetricsConfig{
			Enabled: cfg.Metrics.Enabled,
			Path:    cfg.Metrics.Path,
		})
		log.Info().Str("path", cfg.Metrics.Path).Msg("initialized Prometheus metrics")
	}

	// Initialize health monitor
	var healthMonitor health.Monitor
	if cfg.Health.Enabled {
		healthMonitor = health.NewMonitor(health.HealthConfig{
			Enabled:            cfg.Health.Enabled,
			CheckInterval:      cfg.Health.CheckInterval,
			TokenRefreshBefore: cfg.Health.TokenRefreshBefore,
			Timeout:            cfg.Health.Timeout,
		}, db, circuitMgr, oauthService)
		log.Info().Dur("interval", cfg.Health.CheckInterval).Msg("initialized health monitor")
	}

	// Initialize handlers
	tokenHandler := handler.NewTokenHandler(jwtManager, db, cfg.JWT.DefaultExpiry)
	sessionHandler := handler.NewSessionHandler(db)
	accountHandler := handler.NewAccountHandler(db, oauthService)

	// Use enhanced proxy handler
	enhancedProxyHandler := handler.NewEnhancedProxyHandler(handler.EnhancedProxyConfig{
		Store:       db,
		KeyPool:     keyPool,
		WebURL:      cfg.Claude.WebURL,
		APIURL:      cfg.Claude.APIURL,
		Pool:        httpPool,
		Scheduler:   schedulerSvc,
		Circuit:     circuitMgr,
		Concurrency: concurrencyMgr,
		RateLimit:   rateLimiter,
		Retry:       retryExecutor,
		Metrics:     metricsCollector,
	})

	// Keep legacy handlers for specific endpoints
	webProxyHandler := handler.NewWebProxyHandler(db, cfg.Claude.WebURL)
	apiProxyHandler := handler.NewAPIProxyHandler(keyPool, cfg.Claude.APIURL)

	// Initialize middleware
	jwtMiddleware := middleware.NewJWTMiddleware(jwtManager, db)
	adminMiddleware := middleware.NewAdminMiddleware(cfg.Admin.Key)

	// Setup router
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger())

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Event logging endpoint (Claude Code telemetry - no auth required, just ignore)
	router.POST("/v1/api/event_logging/batch", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	// Prometheus metrics endpoint
	if metricsCollector != nil {
		router.GET(cfg.Metrics.Path, metricsCollector.Handler())
	}

	// Admin API routes (require admin key)
	admin := router.Group("/api")
	admin.Use(adminMiddleware.Auth())
	{
		// Token management
		admin.POST("/token/generate", tokenHandler.Generate)
		admin.GET("/token/list", tokenHandler.List)
		admin.POST("/token/revoke", tokenHandler.Revoke)

		// Account management (replaces session management)
		admin.POST("/account/oauth", accountHandler.CreateOAuthAccount)
		admin.POST("/account/sessionkey", accountHandler.CreateSessionKeyAccount)
		admin.GET("/account/list", accountHandler.ListAccounts)
		admin.GET("/account/:id", accountHandler.GetAccount)
		admin.PUT("/account/:id", accountHandler.UpdateAccount)
		admin.DELETE("/account/:id", accountHandler.DeleteAccount)
		admin.POST("/account/:id/deactivate", accountHandler.DeactivateAccount)
		admin.POST("/account/:id/refresh", accountHandler.RefreshToken)
		admin.POST("/account/:id/check", accountHandler.CheckHealth)

		// Legacy session endpoints (for backward compatibility)
		admin.POST("/session/add", sessionHandler.Add)
		admin.GET("/session/list", sessionHandler.List)
		admin.DELETE("/session/:id", sessionHandler.Delete)
		admin.POST("/session/:id/deactivate", sessionHandler.Deactivate)

		// Key stats (API mode)
		admin.GET("/keys/stats", apiProxyHandler.GetKeyStats)

		// Enhanced stats endpoints
		admin.GET("/stats/pool", func(c *gin.Context) {
			c.JSON(http.StatusOK, httpPool.Stats())
		})
		admin.GET("/stats/circuit", func(c *gin.Context) {
			c.JSON(http.StatusOK, circuitMgr.Stats())
		})
		admin.GET("/stats/concurrency", func(c *gin.Context) {
			c.JSON(http.StatusOK, concurrencyMgr.Stats())
		})
		admin.GET("/stats/ratelimit", func(c *gin.Context) {
			c.JSON(http.StatusOK, rateLimiter.Stats())
		})
		admin.GET("/stats/scheduler", func(c *gin.Context) {
			c.JSON(http.StatusOK, schedulerSvc.Stats())
		})
		admin.GET("/stats/retry", func(c *gin.Context) {
			c.JSON(http.StatusOK, retryExecutor.Stats())
		})
		if healthMonitor != nil {
			admin.GET("/stats/health", func(c *gin.Context) {
				c.JSON(http.StatusOK, healthMonitor.Stats())
			})
		}
	}

	// User API routes (require JWT)
	api := router.Group("/api")
	api.Use(jwtMiddleware.Auth())
	{
		api.GET("/token/info", tokenHandler.Info)
	}

	// OpenAI-compatible endpoints (require JWT) - use enhanced handler
	v1 := router.Group("/v1")
	v1.Use(jwtMiddleware.Auth())
	{
		v1.POST("/chat/completions", enhancedProxyHandler.ChatCompletions)
		v1.GET("/models", enhancedProxyHandler.ListModels)

		// Native Anthropic API proxy - now using enhanced handler for Web/API dual mode
		v1.POST("/messages", enhancedProxyHandler.Messages)
		v1.POST("/messages/count_tokens", apiProxyHandler.CountTokens)

		// Handle double /v1/v1 paths (client has /v1 in base URL)
		v1.POST("/v1/messages", enhancedProxyHandler.Messages)
		v1.POST("/v1/messages/count_tokens", apiProxyHandler.CountTokens)
	}

	// Web mode routes (direct claude.ai proxy)
	webRoutes := router.Group("/web")
	webRoutes.Use(jwtMiddleware.Auth())
	webRoutes.Use(jwtMiddleware.RequireMode("web", "both"))
	{
		webRoutes.POST("/conversations", webProxyHandler.CreateConversation)
		webRoutes.GET("/conversations", webProxyHandler.ListConversations)
		webRoutes.GET("/conversations/:conversation_id", webProxyHandler.GetConversation)
		webRoutes.DELETE("/conversations/:conversation_id", webProxyHandler.DeleteConversation)
		webRoutes.POST("/conversations/:conversation_id/completion", webProxyHandler.SendMessage)
	}

	// Admin UI (embedded SPA)
	adminUI, err := handler.NewAdminUIHandler(web.DistFS, "dist")
	if err != nil {
		log.Warn().Err(err).Msg("failed to initialize admin UI, skipping")
	} else {
		adminUI.RegisterRoutes(router)
		log.Info().Msg("admin UI available at /admin/")
	}

	// Start health monitor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if healthMonitor != nil {
		if err := healthMonitor.Start(ctx); err != nil {
			log.Error().Err(err).Msg("failed to start health monitor")
		}
		defer healthMonitor.Stop()
	}

	// Start server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
	}

	// Graceful shutdown
	go func() {
		log.Info().Str("addr", addr).Msg("starting server")
		log.Info().
			Bool("pool", true).
			Bool("circuit", cfg.Circuit.Enabled).
			Bool("concurrency", true).
			Bool("ratelimit", cfg.RateLimit.Enabled).
			Bool("health", cfg.Health.Enabled).
			Bool("metrics", cfg.Metrics.Enabled).
			Msg("enhanced features enabled")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("failed to start server")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatal().Err(err).Msg("server forced to shutdown")
	}

	log.Info().Msg("server stopped")
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		if raw != "" {
			path = path + "?" + raw
		}

		log.Info().
			Int("status", status).
			Str("method", c.Request.Method).
			Str("path", path).
			Dur("latency", latency).
			Str("ip", c.ClientIP()).
			Msg("request")
	}
}
