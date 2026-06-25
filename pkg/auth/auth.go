// Package auth provides stateless authentication (HS256 JWT bearer tokens) and
// role-based authorization shared across the platform.
//
// The API Gateway is the single point that issues and verifies tokens (OAuth2
// client-credentials shape: client_id + client_secret -> short-lived JWT). It
// then forwards the authenticated subject and role to internal services as
// trusted headers over the private network. Swapping HS256 for RS256 + an
// external OIDC provider is a config change, not a redesign.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Roles understood across the platform.
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// Headers the gateway injects after verifying a token, consumed by services.
const (
	HeaderSubject = "X-Auth-Subject"
	HeaderRole    = "X-Auth-Role"
)

var (
	ErrInvalidCredentials = errors.New("invalid client credentials")
	ErrInvalidToken       = errors.New("invalid or expired token")
)

// Claims is the JWT payload.
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// Token is an issued access token (OAuth2-style response body).
type Token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Role        string `json:"role"`
}

// Authenticator issues and verifies JWTs.
type Authenticator struct {
	secret []byte
	issuer string
	expiry time.Duration
}

// NewAuthenticator constructs the token issuer/verifier.
func NewAuthenticator(secret, issuer string, expiry time.Duration) *Authenticator {
	return &Authenticator{secret: []byte(secret), issuer: issuer, expiry: expiry}
}

// Issue mints a signed token for subject with role.
func (a *Authenticator) Issue(subject, role string) (*Token, error) {
	now := time.Now()
	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    a.issuer,
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(a.expiry)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(a.secret)
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}
	return &Token{
		AccessToken: signed,
		TokenType:   "Bearer",
		ExpiresIn:   int(a.expiry.Seconds()),
		Role:        role,
	}, nil
}

// Verify parses and validates a bearer token, pinning the signing algorithm to
// HS256 to defend against the alg=none / algorithm-confusion attack.
func (a *Authenticator) Verify(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return a.secret, nil
	}, jwt.WithIssuer(a.issuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
