package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenManager signs/verifies access JWTs and mints opaque refresh tokens.
type TokenManager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewTokenManager builds a manager with the given HMAC secret and TTLs.
func NewTokenManager(secret string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func (m *TokenManager) AccessTTL() time.Duration  { return m.accessTTL }
func (m *TokenManager) RefreshTTL() time.Duration { return m.refreshTTL }

// SignAccess issues a signed access token for a user.
func (m *TokenManager) SignAccess(userID, tenantID int64, role, email string, isRoot bool) (string, error) {
	now := time.Now()
	claims := Claims{
		TenantID: tenantID,
		Role:     role,
		Email:    email,
		IsRoot:   isRoot,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// ParseAccess verifies a token string and returns its claims.
func (m *TokenManager) ParseAccess(tokenStr string) (*Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	return &claims, nil
}

// NewRefreshToken returns a fresh opaque refresh token (base64url of 32 random
// bytes) and its sha-256 hash for storage.
func (m *TokenManager) NewRefreshToken() (token string, hash []byte, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, HashToken(token), nil
}

// HashToken returns the sha-256 of a refresh token for constant-shape lookups.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
