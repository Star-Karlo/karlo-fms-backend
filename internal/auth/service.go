package auth

import (
	"context"
	"errors"
	"fmt"
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
	return UserDTO{ID: u.ID, Email: u.Email, FullName: u.FullName, Role: u.Role, Status: u.Status, TenantID: u.TenantID, IsRoot: u.IsRoot}
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

// MinPasswordLen is the floor for a new password.
//
// Length alone, deliberately: complexity rules (a digit, a symbol, mixed case)
// push people towards predictable substitutions and a sticky note, while length
// is what actually costs an attacker. A passphrase clears this easily.
const MinPasswordLen = 10

// ErrWrongPassword is returned when the supplied current password does not
// match. Distinct from ErrInvalidCredentials so the handler can say something
// useful without that leaking into the login path.
var ErrWrongPassword = errors.New("current password is incorrect")

// ChangePassword verifies the current password, sets a new one, and returns a
// fresh token pair.
//
// EVERY EXISTING SESSION IS REVOKED. A password change is what someone does
// when they think a credential is compromised, so leaving other refresh tokens
// alive would defeat the point — the attacker's session would outlive the
// change. The caller is then re-issued immediately, so the person doing it
// stays logged in on this device while every other device is signed out.
func (s *Service) ChangePassword(ctx context.Context, tenantID, userID int64, current, next, userAgent string) (*TokenResponse, error) {
	if len(next) < MinPasswordLen {
		return nil, fmt.Errorf("new password must be at least %d characters", MinPasswordLen)
	}
	if current == next {
		return nil, errors.New("new password must be different from the current one")
	}

	var user store.User
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		u, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return err
		}
		if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)); err != nil {
			return ErrWrongPassword
		}
		// Cost 12, matching internal/users — a hash written here must be
		// indistinguishable from one written at user creation.
		hash, err := bcrypt.GenerateFromPassword([]byte(next), 12)
		if err != nil {
			return err
		}
		if err := q.UpdateUserPassword(ctx, store.UpdateUserPasswordParams{
			ID: userID, PasswordHash: string(hash),
		}); err != nil {
			return err
		}
		// Same transaction as the password write: a revoke that succeeded while
		// the password change rolled back (or vice versa) would leave the two
		// disagreeing about which credentials are live.
		if err := q.RevokeAllUserTokens(ctx, userID); err != nil {
			return err
		}
		u.PasswordHash = string(hash)
		user = u
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.issue(ctx, user, userAgent)
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
	access, err := s.tm.SignAccess(user.ID, user.TenantID, user.Role, user.Email, user.IsRoot)
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
