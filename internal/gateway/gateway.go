// Package gateway is the API Gateway: the single public entrypoint to the
// platform. It terminates auth (issues + verifies JWTs), enforces RBAC and rate
// limits at the edge, and reverse-proxies to internal services, injecting the
// authenticated identity as trusted headers.
//
// Keeping auth, rate limiting, and routing here means internal services stay
// small and unauthenticated-on-the-private-network — they trust the gateway.
// This is the API-gateway / edge pattern the JD calls out.
package gateway

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/AmeerHamza2/web3-event-platform/pkg/auth"
	"github.com/AmeerHamza2/web3-event-platform/pkg/httpx"
)

// Client is a registered API client (machine-to-machine credential).
type Client struct {
	Secret string
	Role   string
}

// Config configures the gateway.
type Config struct {
	Auth      *auth.Authenticator
	Clients   map[string]Client // clientID -> credential
	UserURL   string            // user service base URL
	WalletURL string            // wallet service base URL
	RateLimit *RateLimiter
}

// Gateway is the assembled edge handler.
type Gateway struct {
	cfg         Config
	userProxy   *httputil.ReverseProxy
	walletProxy *httputil.ReverseProxy
}

// New builds the gateway, wiring reverse proxies to the upstream services.
func New(cfg Config) (*Gateway, error) {
	uu, err := url.Parse(cfg.UserURL)
	if err != nil {
		return nil, err
	}
	wu, err := url.Parse(cfg.WalletURL)
	if err != nil {
		return nil, err
	}
	return &Gateway{
		cfg:         cfg,
		userProxy:   newProxy(uu),
		walletProxy: newProxy(wu),
	}, nil
}

// newProxy builds a reverse proxy that strips the /api/v1 public prefix so the
// upstream sees its own native paths (/users, /wallets).
func newProxy(target *url.URL) *httputil.ReverseProxy {
	p := httputil.NewSingleHostReverseProxy(target)
	orig := p.Director
	p.Director = func(r *http.Request) {
		orig(r)
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api/v1")
		r.Host = target.Host
	}
	return p
}

// Handler returns the fully-wired gateway HTTP handler.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public: health + token issuance.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/v1/auth/token", g.issueToken)

	// Protected: everything under /api/v1/{users,wallets} requires a valid token.
	mux.Handle("/api/v1/users", g.authed(g.userProxy))
	mux.Handle("/api/v1/users/", g.authed(g.userProxy))
	mux.Handle("/api/v1/wallets", g.authed(g.walletProxy))
	mux.Handle("/api/v1/wallets/", g.authed(g.walletProxy))

	return mux
}

type tokenRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// issueToken implements the OAuth2 client-credentials grant.
func (g *Gateway) issueToken(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	client, ok := g.cfg.Clients[req.ClientID]
	if !ok || client.Secret != req.ClientSecret {
		httpx.Error(w, http.StatusUnauthorized, "invalid client credentials")
		return
	}
	tok, err := g.cfg.Auth.Issue(req.ClientID, client.Role)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	httpx.JSON(w, http.StatusOK, tok)
}

// authed verifies the bearer token, applies the rate limit, and forwards the
// authenticated subject + role to the upstream as trusted headers.
func (g *Gateway) authed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.cfg.RateLimit != nil && !g.cfg.RateLimit.Allow(clientIP(r)) {
			httpx.Error(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		bearer := r.Header.Get("Authorization")
		raw, ok := strings.CutPrefix(bearer, "Bearer ")
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := g.cfg.Auth.Verify(raw)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		// Strip any client-supplied identity headers, then set the verified ones.
		r.Header.Del(auth.HeaderSubject)
		r.Header.Del(auth.HeaderRole)
		r.Header.Set(auth.HeaderSubject, claims.Subject)
		r.Header.Set(auth.HeaderRole, claims.Role)

		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if host, _, ok := strings.Cut(r.RemoteAddr, ":"); ok {
		return host
	}
	return r.RemoteAddr
}
