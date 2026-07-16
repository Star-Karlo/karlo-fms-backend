package auth

import "github.com/golang-jwt/jwt/v5"

// Role hierarchy: admin ⊇ manager ⊇ operator.
const (
	RoleOperator = "operator"
	RoleManager  = "manager"
	RoleAdmin    = "admin"
)

var roleRank = map[string]int{RoleOperator: 1, RoleManager: 2, RoleAdmin: 3}

// AtLeast reports whether have satisfies the want role (by hierarchy).
func AtLeast(have, want string) bool {
	return roleRank[have] >= roleRank[want] && roleRank[want] > 0
}

// Claims is the JWT access-token payload.
type Claims struct {
	TenantID int64  `json:"tid"`
	Role     string `json:"role"`
	Email    string `json:"email"`
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
