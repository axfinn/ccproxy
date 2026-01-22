package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"ccproxy/internal/service"
	"ccproxy/internal/store"
)

type AccountHandler struct {
	store        *store.Store
	oauthService *service.OAuthService
}

func NewAccountHandler(store *store.Store, oauthService *service.OAuthService) *AccountHandler {
	return &AccountHandler{
		store:        store,
		oauthService: oauthService,
	}
}

// CreateOAuthAccount creates a new OAuth account via login flow
func (h *AccountHandler) CreateOAuthAccount(c *gin.Context) {
	var req service.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.oauthService.Login(req)
	if err != nil {
		log.Error().Err(err).Msg("OAuth login failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"account_id":      result.AccountID,
		"organization_id": result.OrganizationID,
		"expires_at":      result.ExpiresAt,
		"message":         "OAuth login successful",
	})
}

// CreateSessionKeyAccount creates a new session key account (legacy support)
func (h *AccountHandler) CreateSessionKeyAccount(c *gin.Context) {
	var req struct {
		Name           string `json:"name" binding:"required"`
		SessionKey     string `json:"session_key" binding:"required"`
		OrganizationID string `json:"organization_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	account := &store.Account{
		ID:             "acc_" + uuid.New().String(),
		Name:           req.Name,
		Type:           store.AccountTypeSessionKey,
		OrganizationID: req.OrganizationID,
		Credentials: store.Credentials{
			SessionKey: req.SessionKey,
		},
		CreatedAt:    time.Now(),
		IsActive:     true,
		HealthStatus: "unknown",
	}

	if err := h.store.CreateAccount(account); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":              account.ID,
		"name":            account.Name,
		"type":            account.Type,
		"organization_id": account.OrganizationID,
		"is_active":       account.IsActive,
		"created_at":      account.CreatedAt,
	})
}

// ListAccounts lists all accounts
func (h *AccountHandler) ListAccounts(c *gin.Context) {
	accounts, err := h.store.ListAccounts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list accounts"})
		return
	}

	// Remove sensitive credentials before sending
	response := make([]gin.H, len(accounts))
	for i, acc := range accounts {
		response[i] = gin.H{
			"id":              acc.ID,
			"name":            acc.Name,
			"type":            acc.Type,
			"organization_id": acc.OrganizationID,
			"expires_at":      acc.ExpiresAt,
			"created_at":      acc.CreatedAt,
			"last_used_at":    acc.LastUsedAt,
			"is_active":       acc.IsActive,
			"last_check_at":   acc.LastCheckAt,
			"health_status":   acc.HealthStatus,
			"error_count":     acc.ErrorCount,
			"success_count":   acc.SuccessCount,
		}
	}

	c.JSON(http.StatusOK, response)
}

// GetAccount gets a specific account
func (h *AccountHandler) GetAccount(c *gin.Context) {
	id := c.Param("id")
	account, err := h.store.GetAccount(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get account"})
		return
	}
	if account == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":              account.ID,
		"name":            account.Name,
		"type":            account.Type,
		"organization_id": account.OrganizationID,
		"expires_at":      account.ExpiresAt,
		"created_at":      account.CreatedAt,
		"last_used_at":    account.LastUsedAt,
		"is_active":       account.IsActive,
		"last_check_at":   account.LastCheckAt,
		"health_status":   account.HealthStatus,
		"error_count":     account.ErrorCount,
		"success_count":   account.SuccessCount,
	})
}

// UpdateAccount updates an account
func (h *AccountHandler) UpdateAccount(c *gin.Context) {
	id := c.Param("id")
	account, err := h.store.GetAccount(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get account"})
		return
	}
	if account == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	var req struct {
		Name     string `json:"name"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != "" {
		account.Name = req.Name
	}
	if req.IsActive != nil {
		account.IsActive = *req.IsActive
	}

	if err := h.store.UpdateAccount(account); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "account updated"})
}

// DeleteAccount deletes an account
func (h *AccountHandler) DeleteAccount(c *gin.Context) {
	id := c.Param("id")
	if err := h.store.DeleteAccount(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "account deleted"})
}

// DeactivateAccount deactivates an account
func (h *AccountHandler) DeactivateAccount(c *gin.Context) {
	id := c.Param("id")
	if err := h.store.DeactivateAccount(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to deactivate account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "account deactivated"})
}

// RefreshToken manually refreshes an OAuth account's token
func (h *AccountHandler) RefreshToken(c *gin.Context) {
	id := c.Param("id")
	account, err := h.store.GetAccount(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get account"})
		return
	}
	if account == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	if !account.IsOAuth() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account is not an OAuth account"})
		return
	}

	if err := h.oauthService.RefreshToken(account); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "token refreshed",
		"expires_at": account.ExpiresAt,
	})
}

// CheckHealth performs a health check on an account
func (h *AccountHandler) CheckHealth(c *gin.Context) {
	id := c.Param("id")
	account, err := h.store.GetAccount(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get account"})
		return
	}
	if account == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	if err := h.oauthService.CheckHealth(account); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "unhealthy",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"message": "account is healthy",
	})
}
