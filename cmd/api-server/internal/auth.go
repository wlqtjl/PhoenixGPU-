// Package internal — bearer token authentication middleware.
package internal

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// publicPaths are endpoints that do not require authentication (health/readiness probes).
var publicPaths = map[string]bool{
	"/healthz": true,
	"/readyz":  true,
	"/metrics": true,
}

// withAuth adds bearer token authentication to protected endpoints.
// If no tokens are configured, authentication is disabled (backward-compatible).
func withAuth(cfg RouterConfig, next http.Handler) http.Handler {
	if len(cfg.AuthTokens) == 0 {
		return next // auth disabled
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for public endpoints
		if publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			errResp(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}

		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			errResp(w, http.StatusUnauthorized, "invalid Authorization header format")
			return
		}

		token := strings.TrimPrefix(authHeader, bearerPrefix)
		if !constantTimeTokenMatch(token, cfg.AuthTokens) {
			errResp(w, http.StatusForbidden, "invalid or expired token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// constantTimeTokenMatch checks whether token matches any entry in validTokens
// using constant-time comparison to prevent timing side-channel attacks.
func constantTimeTokenMatch(token string, validTokens map[string]bool) bool {
	tokenBytes := []byte(token)
	for t := range validTokens {
		if subtle.ConstantTimeCompare(tokenBytes, []byte(t)) == 1 {
			return true
		}
	}
	return false
}
