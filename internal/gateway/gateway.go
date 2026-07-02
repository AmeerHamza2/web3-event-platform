// Package gateway is the platform's public edge: it issues and verifies JWTs,
// enforces RBAC and rate limits, and reverse-proxies to internal services,
// forwarding the verified identity as trusted headers.
package gateway

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/AmeerHamza2/web3-event-platform/pkg/auth"
	"github.com/AmeerHamza2/web3-event-platform/pkg/httpx"
)

// Client is a registered machine-to-machine credential.
type Client struct {
	Secret string
	Role   string
}

type Config struct {
	Auth      *auth.Authenticator
	Clients   map[string]Client
	UserURL   string
	WalletURL string
	MarginURL string
	RateLimit *RateLimiter
}

type Gateway struct {
	cfg         Config
	userProxy   *httputil.ReverseProxy
	walletProxy *httputil.ReverseProxy
	marginProxy *httputil.ReverseProxy
}

func New(cfg Config) (*Gateway, error) {
	uu, err := url.Parse(cfg.UserURL)
	if err != nil {
		return nil, err
	}
	wu, err := url.Parse(cfg.WalletURL)
	if err != nil {
		return nil, err
	}
	mu, err := url.Parse(cfg.MarginURL)
	if err != nil {
		return nil, err
	}
	return &Gateway{
		cfg:         cfg,
		userProxy:   newProxy(uu),
		walletProxy: newProxy(wu),
		marginProxy: newProxy(mu),
	}, nil
}

// newProxy strips the /api/v1 public prefix so upstreams see their native paths.
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

func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/v1/auth/token", g.issueToken)

	mux.Handle("/api/v1/users", g.authed(g.userProxy))
	mux.Handle("/api/v1/users/", g.authed(g.userProxy))
	mux.Handle("/api/v1/wallets", g.authed(g.walletProxy))
	mux.Handle("/api/v1/wallets/", g.authed(g.walletProxy))
	mux.Handle("/api/v1/margin/", g.authed(g.marginProxy))

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

// authed rate-limits, verifies the bearer token, and forwards the verified
// subject and role to the upstream.
func (g *Gateway) authed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.cfg.RateLimit != nil && !g.cfg.RateLimit.Allow(clientIP(r)) {
			httpx.Error(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := g.cfg.Auth.Verify(raw)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		// Overwrite any client-supplied identity headers with the verified ones.
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
