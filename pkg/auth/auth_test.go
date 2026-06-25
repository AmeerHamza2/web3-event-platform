package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newTestAuth() *Authenticator {
	return NewAuthenticator("test-secret", "test-issuer", time.Hour)
}

func TestIssueVerifyRoundtrip(t *testing.T) {
	a := newTestAuth()
	tok, err := a.Issue("client-1", RoleAdmin)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := a.Verify(tok.AccessToken)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != "client-1" || claims.Role != RoleAdmin {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

// A token signed with "alg":"none" must be rejected — the classic JWT
// algorithm-confusion attack. This is a security regression guard.
func TestVerifyRejectsAlgNone(t *testing.T) {
	a := newTestAuth()
	claims := Claims{Role: RoleAdmin, RegisteredClaims: jwt.RegisteredClaims{Issuer: "test-issuer"}}
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	raw, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}
	if _, err := a.Verify(raw); err == nil {
		t.Fatal("expected alg=none token to be rejected, but it was accepted")
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	a := newTestAuth()
	tok, _ := a.Issue("c", RoleUser)
	other := NewAuthenticator("different-secret", "test-issuer", time.Hour)
	if _, err := other.Verify(tok.AccessToken); err == nil {
		t.Fatal("expected token signed with a different secret to be rejected")
	}
}
