package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ccproxy/internal/middleware"
	"ccproxy/internal/store"
	"ccproxy/pkg/jwt"
)

type TokenHandler struct {
	jwtManager    *jwt.Manager
	store         *store.Store
	defaultExpiry time.Duration
}

func NewTokenHandler(jwtManager *jwt.Manager, store *store.Store, defaultExpiry time.Duration) *TokenHandler {
	return &TokenHandler{
		jwtManager:    jwtManager,
		store:         store,
		defaultExpiry: defaultExpiry,
	}
}

type GenerateTokenRequest struct {
	Name      string `json:"name" binding:"required"`
	ExpiresIn string `json:"expires_in"` // e.g., "720h", "30d"
	Mode      string `json:"mode"`       // "web", "api", or "both"
}

type GenerateTokenResponse struct {
	Token     string    `json:"token"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Mode      string    `json:"mode"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (h *TokenHandler) Generate(c *gin.Context) {
	var req GenerateTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Parse expiry duration
	expiry := h.defaultExpiry
	if req.ExpiresIn != "" {
		d, err := time.ParseDuration(req.ExpiresIn)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid expires_in format"})
			return
		}
		expiry = d
	}

	// Default mode
	mode := req.Mode
	if mode == "" {
		mode = "both"
	}
	if mode != "web" && mode != "api" && mode != "both" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode, must be 'web', 'api', or 'both'"})
		return
	}

	// Generate JWT
	tokenString, tokenInfo, err := h.jwtManager.Generate(req.Name, mode, expiry)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	// Store token in database
	dbToken := &store.Token{
		ID:        tokenInfo.ID,
		UserName:  tokenInfo.UserName,
		Mode:      mode,
		CreatedAt: tokenInfo.IssuedAt,
		ExpiresAt: tokenInfo.ExpiresAt,
	}
	if err := h.store.CreateToken(dbToken); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store token"})
		return
	}

	c.JSON(http.StatusOK, GenerateTokenResponse{
		Token:     tokenString,
		ID:        tokenInfo.ID,
		Name:      req.Name,
		Mode:      mode,
		ExpiresAt: tokenInfo.ExpiresAt,
	})
}

type TokenListResponse struct {
	Tokens []*TokenInfo `json:"tokens"`
}

type TokenInfo struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Mode       string     `json:"mode"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	IsValid    bool       `json:"is_valid"`
}

func (h *TokenHandler) List(c *gin.Context) {
	tokens, err := h.store.ListTokens()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list tokens"})
		return
	}

	now := time.Now()
	response := make([]*TokenInfo, len(tokens))
	for i, t := range tokens {
		isValid := t.RevokedAt == nil && t.ExpiresAt.After(now)
		response[i] = &TokenInfo{
			ID:         t.ID,
			Name:       t.UserName,
			Mode:       t.Mode,
			CreatedAt:  t.CreatedAt,
			ExpiresAt:  t.ExpiresAt,
			RevokedAt:  t.RevokedAt,
			LastUsedAt: t.LastUsedAt,
			IsValid:    isValid,
		}
	}

	c.JSON(http.StatusOK, TokenListResponse{Tokens: response})
}

func (h *TokenHandler) Info(c *gin.Context) {
	tokenID, exists := c.Get(middleware.ContextKeyTokenID)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	token, err := h.store.GetToken(tokenID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get token info"})
		return
	}

	if token == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}

	now := time.Now()
	isValid := token.RevokedAt == nil && token.ExpiresAt.After(now)

	c.JSON(http.StatusOK, TokenInfo{
		ID:         token.ID,
		Name:       token.UserName,
		Mode:       token.Mode,
		CreatedAt:  token.CreatedAt,
		ExpiresAt:  token.ExpiresAt,
		RevokedAt:  token.RevokedAt,
		LastUsedAt: token.LastUsedAt,
		IsValid:    isValid,
	})
}

type RevokeTokenRequest struct {
	ID string `json:"id" binding:"required"`
}

func (h *TokenHandler) Revoke(c *gin.Context) {
	var req RevokeTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.store.RevokeToken(req.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "token revoked successfully"})
}
