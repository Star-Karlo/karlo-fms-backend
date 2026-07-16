package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/store"
)

// Sentinel errors surfaced to the HTTP layer as 401.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidToken       = errors.New("invalid or expired refresh token")
)

// Service implements login, token refresh (with rotation + reuse detection),
// and logout. All lookups run unscoped (SystemTx) because they precede the
// tenant context; the login-bootstrap RLS policy permits this.
type Service struct {
	pool *db.Pool
	tm   *TokenManager
}

// NewService builds the auth service.
func NewService(pool *db.Pool, tm *TokenManager) *Service {
	return &Service{pool: pool, tm: tm}
}

func toUserDTO(u store.User) UserDTO {
	return UserDTO{ID: u.ID, Email: u.Email, FullName: u.FullName, Role: u.Role, Status: u.Status, TenantID: u.TenantID}
}

// Login verifies credentials and issues an access+refresh token pair.
func (s *Service) Login(ctx context.Context, email, password, userAgent string) (*TokenResponse, error) {
	var user store.User
	err := db.SystemTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		u, err := q.GetUserByEmail(ctx, email)
		if err != nil {
			return ErrInvalidCredentials
		}
		if u.Status != "active" {
			return ErrInvalidCredentials
		}
		if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
			return ErrInvalidCredentials
		}
		_ = q.TouchUserLogin(ctx, u.ID)
		user = u
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.issue(ctx, user, userAgent)
}

// Refresh rotates a refresh token: the presented token is revoked and a fresh
// pair issued. A replayed (already-revoked) token revokes the whole user chain.
func (s *Service) Refresh(ctx context.Context, refreshToken, userAgent string) (*TokenResponse, error) {
	hash := HashToken(refreshToken)
	var user store.User
	err := db.SystemTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		rt, err := q.GetRefreshTokenByHash(ctx, hash)
		if err != nil {
			return ErrInvalidToken
		}
		// Reuse detection: a revoked token being presented again means it leaked.
		if rt.RevokedAt.Valid {
			_ = q.RevokeAllUserTokens(ctx, rt.UserID)
			return ErrInvalidToken
		}
		if rt.ExpiresAt.Valid && rt.ExpiresAt.Time.Before(time.Now()) {
			return ErrInvalidToken
		}
		u, err := q.GetUserByID(ctx, rt.UserID)
		if err != nil || u.Status != "active" {
			return ErrInvalidToken
		}
		if err := q.RevokeRefreshToken(ctx, store.RevokeRefreshTokenParams{ID: rt.ID}); err != nil {
			return err
		}
		user = u
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.issue(ctx, user, userAgent)
}

// Logout revokes the presented refresh token (idempotent).
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	hash := HashToken(refreshToken)
	return db.SystemTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		rt, err := q.GetRefreshTokenByHash(ctx, hash)
		if err != nil {
			return nil // unknown token: nothing to do
		}
		return q.RevokeRefreshToken(ctx, store.RevokeRefreshTokenParams{ID: rt.ID})
	})
}

// Me returns the current user by id (tenant-scoped).
func (s *Service) Me(ctx context.Context, tenantID, userID int64) (*UserDTO, error) {
	var dto UserDTO
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		u, err := store.New(tx).GetUserByID(ctx, userID)
		if err != nil {
			return err
		}
		dto = toUserDTO(u)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &dto, nil
}

// issue mints an access token and a stored refresh token for a user.
func (s *Service) issue(ctx context.Context, user store.User, userAgent string) (*TokenResponse, error) {
	access, err := s.tm.SignAccess(user.ID, user.TenantID, user.Role, user.Email)
	if err != nil {
		return nil, err
	}
	refresh, hash, err := s.tm.NewRefreshToken()
	if err != nil {
		return nil, err
	}
	var ua *string
	if userAgent != "" {
		ua = &userAgent
	}
	err = db.SystemTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := store.New(tx).CreateRefreshToken(ctx, store.CreateRefreshTokenParams{
			TenantID:  user.TenantID,
			UserID:    user.ID,
			TokenHash: hash,
			ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(s.tm.RefreshTTL()), Valid: true},
			UserAgent: ua,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return &TokenResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(s.tm.AccessTTL().Seconds()),
		User:         toUserDTO(user),
	}, nil
}
