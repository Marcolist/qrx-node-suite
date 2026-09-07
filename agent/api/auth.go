package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

type actorCtxKey struct{}

// actorFromContext returns the authenticated principal for the current
// request, set by RequireAdmin. There is currently only one principal (the
// holder of the shared admin token) -- see docs/security.md for why
// per-user accounts are a documented gap, not silently pretended away.
func actorFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(actorCtxKey{}).(string); ok {
		return v
	}
	return "unknown"
}

// RequireAdmin gates an administrative endpoint
// (docs/updates.md#admin-authorization): it must be authenticated, and
// unreachable anonymously. An empty adminToken means administrative
// endpoints are entirely DISABLED -- never silently "open" -- matching
// docs/architecture.md principle 9 (fail safely) and
// docs/security.md's hard guarantees.
func RequireAdmin(adminToken string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminToken == "" {
			httpError(w, http.StatusForbidden, "administrative endpoints are disabled: no admin token configured")
			return
		}
		const prefix = "Bearer "
		got := r.Header.Get("Authorization")
		if !strings.HasPrefix(got, prefix) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		presented := strings.TrimPrefix(got, prefix)
		if subtle.ConstantTimeCompare([]byte(presented), []byte(adminToken)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpError(w, http.StatusUnauthorized, "invalid admin token")
			return
		}
		ctx := context.WithValue(r.Context(), actorCtxKey{}, "admin")
		next(w, r.WithContext(ctx))
	}
}
