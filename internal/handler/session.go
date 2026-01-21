package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ccproxy/internal/store"
)

type SessionHandler struct {
	store *store.Store
}

func NewSessionHandler(store *store.Store) *SessionHandler {
	return &SessionHandler{store: store}
}

type AddSessionRequest struct {
	Name           string  `json:"name" binding:"required"`
	SessionKey     string  `json:"session_key" binding:"required"`
	OrganizationID string  `json:"organization_id"`
	ExpiresIn      *string `json:"expires_in"` // e.g., "720h"
}

type SessionResponse struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	OrganizationID string     `json:"organization_id"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	IsActive       bool       `json:"is_active"`
}

func (h *SessionHandler) Add(c *gin.Context) {
	var req AddSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session := &store.Session{
		ID:             uuid.New().String(),
		Name:           req.Name,
		SessionKey:     req.SessionKey,
		OrganizationID: req.OrganizationID,
		CreatedAt:      time.Now(),
		IsActive:       true,
	}

	// Parse optional expiry
	if req.ExpiresIn != nil && *req.ExpiresIn != "" {
		d, err := time.ParseDuration(*req.ExpiresIn)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid expires_in format"})
			return
		}
		expiresAt := time.Now().Add(d)
		session.ExpiresAt = &expiresAt
	}

	if err := h.store.CreateSession(session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	c.JSON(http.StatusOK, SessionResponse{
		ID:             session.ID,
		Name:           session.Name,
		OrganizationID: session.OrganizationID,
		CreatedAt:      session.CreatedAt,
		ExpiresAt:      session.ExpiresAt,
		IsActive:       session.IsActive,
	})
}

type SessionListResponse struct {
	Sessions []*SessionResponse `json:"sessions"`
}

func (h *SessionHandler) List(c *gin.Context) {
	sessions, err := h.store.ListSessions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list sessions"})
		return
	}

	response := make([]*SessionResponse, len(sessions))
	for i, s := range sessions {
		response[i] = &SessionResponse{
			ID:             s.ID,
			Name:           s.Name,
			OrganizationID: s.OrganizationID,
			CreatedAt:      s.CreatedAt,
			ExpiresAt:      s.ExpiresAt,
			LastUsedAt:     s.LastUsedAt,
			IsActive:       s.IsActive,
		}
	}

	c.JSON(http.StatusOK, SessionListResponse{Sessions: response})
}

func (h *SessionHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session id required"})
		return
	}

	if err := h.store.DeleteSession(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "session deleted successfully"})
}

func (h *SessionHandler) Deactivate(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session id required"})
		return
	}

	if err := h.store.DeactivateSession(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to deactivate session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "session deactivated successfully"})
}
