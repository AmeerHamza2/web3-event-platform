// Package auth issues and verifies HS256 JWT bearer tokens and defines the
// platform's roles. The gateway issues tokens via the OAuth2 client-credentials
// grant and forwards the verified subject and role to internal services.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Roles.
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// Identity headers the gateway sets after verifying a token.
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

// Token is an OAuth2-style token response.
type Token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Role        string `json:"role"`
}

// Authenticator issues and verifies tokens.
type Authenticator struct {
	secret []byte
	issuer string
	expiry time.Duration
}

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
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}
	return &Token{AccessToken: signed, TokenType: "Bearer", ExpiresIn: int(a.expiry.Seconds()), Role: role}, nil
}

// Verify validates a token, pinning the algorithm to HS256 to reject alg=none
// and algorithm-confusion attacks.
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
