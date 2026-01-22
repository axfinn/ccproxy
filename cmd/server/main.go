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

	"ccproxy/internal/config"
	"ccproxy/internal/handler"
	"ccproxy/internal/loadbalancer"
	"ccproxy/internal/middleware"
	"ccproxy/internal/service"
	"ccproxy/internal/store"
	"ccproxy/pkg/jwt"
	"ccproxy/web"
)

func main() {
	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(zerolog.DebugLevel) // Enable debug logging
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load configuration")
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

	// Initialize handlers
	tokenHandler := handler.NewTokenHandler(jwtManager, db, cfg.JWT.DefaultExpiry)
	sessionHandler := handler.NewSessionHandler(db)
	accountHandler := handler.NewAccountHandler(db, oauthService)
	proxyHandler := handler.NewProxyHandler(db, keyPool, cfg.Claude.WebURL, cfg.Claude.APIURL)
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
	}

	// User API routes (require JWT)
	api := router.Group("/api")
	api.Use(jwtMiddleware.Auth())
	{
		api.GET("/token/info", tokenHandler.Info)
	}

	// OpenAI-compatible endpoints (require JWT)
	v1 := router.Group("/v1")
	v1.Use(jwtMiddleware.Auth())
	{
		v1.POST("/chat/completions", proxyHandler.ChatCompletions)
		v1.GET("/models", proxyHandler.ListModels)

		// Native Anthropic API proxy
		v1.POST("/messages", apiProxyHandler.Messages)
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
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("failed to start server")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
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
