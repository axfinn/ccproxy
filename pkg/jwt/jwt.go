package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token has expired")
)

type Claims struct {
	UserName string `json:"name"`
	Mode     string `json:"mode"`
	jwt.RegisteredClaims
}

type TokenInfo struct {
	ID        string    `json:"id"`
	UserName  string    `json:"user_name"`
	Mode      string    `json:"mode"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Manager struct {
	secret []byte
	issuer string
}

func NewManager(secret string, issuer string) *Manager {
	return &Manager{
		secret: []byte(secret),
		issuer: issuer,
	}
}

func (m *Manager) Generate(userName string, mode string, expiry time.Duration) (string, *TokenInfo, error) {
	tokenID := uuid.New().String()
	now := time.Now()
	expiresAt := now.Add(expiry)

	claims := Claims{
		UserName: userName,
		Mode:     mode,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        tokenID,
			Subject:   userName,
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(m.secret)
	if err != nil {
		return "", nil, err
	}

	info := &TokenInfo{
		ID:        tokenID,
		UserName:  userName,
		Mode:      mode,
		IssuedAt:  now,
		ExpiresAt: expiresAt,
	}

	return tokenString, info, nil
}

func (m *Manager) Validate(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, ErrInvalidToken
}

func (m *Manager) GetTokenID(tokenString string) (string, error) {
	claims, err := m.Validate(tokenString)
	if err != nil {
		return "", err
	}
	return claims.ID, nil
}
