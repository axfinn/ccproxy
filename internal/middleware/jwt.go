package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ccproxy/internal/store"
	"ccproxy/pkg/jwt"
)

const (
	ContextKeyTokenID   = "token_id"
	ContextKeyUserName  = "user_name"
	ContextKeyTokenMode = "token_mode"
	ContextKeyClaims    = "claims"
)

type JWTMiddleware struct {
	jwtManager *jwt.Manager
	store      *store.Store
}

func NewJWTMiddleware(jwtManager *jwt.Manager, store *store.Store) *JWTMiddleware {
	return &JWTMiddleware{
		jwtManager: jwtManager,
		store:      store,
	}
}

func (m *JWTMiddleware) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := extractToken(c)
		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing authorization token",
			})
			return
		}

		claims, err := m.jwtManager.Validate(tokenString)
		if err != nil {
			status := http.StatusUnauthorized
			message := "invalid token"
			if err == jwt.ErrExpiredToken {
				message = "token has expired"
			}
			c.AbortWithStatusJSON(status, gin.H{
				"error": message,
			})
			return
		}

		// Check if token is revoked in database
		token, err := m.store.ValidateToken(claims.ID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "failed to validate token",
			})
			return
		}

		if token == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "token is revoked or expired",
			})
			return
		}

		// Update last used time
		go m.store.UpdateTokenLastUsed(claims.ID)

		// Set context values
		c.Set(ContextKeyTokenID, claims.ID)
		c.Set(ContextKeyUserName, claims.UserName)
		c.Set(ContextKeyTokenMode, claims.Mode)
		c.Set(ContextKeyClaims, claims)

		c.Next()
	}
}

func (m *JWTMiddleware) RequireMode(modes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenMode, exists := c.Get(ContextKeyTokenMode)
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			return
		}

		mode := tokenMode.(string)
		if mode == "both" {
			c.Next()
			return
		}

		for _, m := range modes {
			if mode == m {
				c.Next()
				return
			}
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "token does not have permission for this mode",
		})
	}
}

type AdminMiddleware struct {
	adminKey string
}

func NewAdminMiddleware(adminKey string) *AdminMiddleware {
	return &AdminMiddleware{adminKey: adminKey}
}

func (m *AdminMiddleware) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("X-Admin-Key")
		if key == "" {
			key = c.Query("admin_key")
		}

		if key == "" || key != m.adminKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or missing admin key",
			})
			return
		}

		c.Next()
	}
}

func extractToken(c *gin.Context) string {
	// Check Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
		// Also support just the token without "Bearer" prefix
		return authHeader
	}

	// Check x-api-key header (common for API clients)
	if apiKey := c.GetHeader("x-api-key"); apiKey != "" {
		return apiKey
	}

	// Check query parameter
	if token := c.Query("token"); token != "" {
		return token
	}

	return ""
}
