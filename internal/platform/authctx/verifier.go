package authctx

import (
	"time"
	"crypto/rsa"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// Verifier verifies tokens locally against the authentication service's public
// key. Every service except the authentication service uses this.
type Verifier struct {
	pub *rsa.PublicKey
}

// NewVerifier builds a verifier from a PEM-encoded RSA public key.
func NewVerifier(pemBytes []byte) (*Verifier, error) {
	if len(pemBytes) == 0 {
		return nil, errors.New("authctx: empty public key")
	}
	pub, err := jwt.ParseRSAPublicKeyFromPEM(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("authctx: parse public key: %w", err)
	}
	return &Verifier{pub: pub}, nil
}

// Verify parses and validates a token, returning the principal it asserts.
func (v *Verifier) Verify(token string) (Principal, error) {
	claims, err := ParseClaims(token, func(t *jwt.Token) (interface{}, error) {
		return v.pub, nil
	})
	if err != nil {
		return Principal{}, err
	}
	if claims.Principal.UserID == "" {
		return Principal{}, errors.New("authctx: token has no subject")
	}
	return claims.Principal, nil
}

// VerifyWithIssuedAt is Verify plus the token's issue time, which the
// revocation rule needs ("revoked at T" only covers tokens issued before T).
func (v *Verifier) VerifyWithIssuedAt(token string) (Principal, time.Time, error) {
	claims, err := ParseClaims(token, func(t *jwt.Token) (interface{}, error) {
		return v.pub, nil
	})
	if err != nil {
		return Principal{}, time.Time{}, err
	}
	if claims.Principal.UserID == "" {
		return Principal{}, time.Time{}, errors.New("authctx: token has no subject")
	}
	var iat time.Time
	if claims.IssuedAt != nil {
		iat = claims.IssuedAt.Time
	}
	return claims.Principal, iat, nil
}
