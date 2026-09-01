package auth

import "github.com/golang-jwt/jwt/v5"

// Role hierarchy: platform_admin ⊇ admin ⊇ manager ⊇ operator.
//
// RolePlatformAdmin is the operator of the SERVICE, not of a fleet: it is the
// only role permitted to create tenants and provision their first login.
//
// It has to sit above admin rather than reuse it. Every customer's own
// administrator holds RoleAdmin — gating tenant creation on that would let one
// customer provision tenants and users for the whole platform, which is a
// privilege escalation across the isolation boundary the whole RLS design
// exists to enforce.
//
// The users.role CHECK constraint is widened to allow it (migration 0027);
// the first platform admin is promoted there too.
const (
	RoleOperator      = "operator"
	RoleManager       = "manager"
	RoleAdmin         = "admin"
	RolePlatformAdmin = "platform_admin"
)

var roleRank = map[string]int{RoleOperator: 1, RoleManager: 2, RoleAdmin: 3, RolePlatformAdmin: 4}

// AtLeast reports whether have satisfies the want role (by hierarchy).
func AtLeast(have, want string) bool {
	return roleRank[have] >= roleRank[want] && roleRank[want] > 0
}

// Claims is the JWT access-token payload.
type Claims struct {
	TenantID int64  `json:"tid"`
	Role     string `json:"role"`
	Email    string `json:"email"`
	// IsRoot marks the top-tier ("platform admin admin"). Only a root platform_admin
	// may create/edit/delete other platform_admin accounts; a child platform_admin
	// has cross-vendor visibility but its actions are bounded by its custom role.
	IsRoot bool `json:"root,omitempty"`
	jwt.RegisteredClaims
}

// UserDTO is the public shape of a user (never includes the password hash).
type UserDTO struct {
	ID       int64   `json:"id"`
	Email    string  `json:"email"`
	FullName *string `json:"full_name"`
	Role     string  `json:"role"`
	Status   string  `json:"status"`
	TenantID int64   `json:"tenant_id"`
	IsRoot   bool    `json:"is_root"`
}

// LoginRequest is the /v1/auth/login body.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RefreshRequest is the /v1/auth/refresh body.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// TokenResponse is returned by login and refresh.
type TokenResponse struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresIn    int     `json:"expires_in"` // access token lifetime, seconds
	User         UserDTO `json:"user"`
}
