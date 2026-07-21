package admin

import (
	"context"
	"errors"
	"log"
	"net/http"

	"pjsk/backend/internal/logsafe"
)

type contextKey struct{}

func (h *Handler) RequireAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenHash, ok := sessionHashFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		account, err := h.store.FindBySession(r.Context(), tokenHash)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				log.Printf("find admin session: %s", logsafe.Category(err))
			}
			h.clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		if account.MustChangePassword && !passwordChangeExemptPath(r.URL.Path) {
			writeError(w, http.StatusForbidden, passwordChangeRequiredMessage)
			return
		}

		ctx := context.WithValue(r.Context(), contextKey{}, account)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// passwordChangeRequiredMessage is the machine-readable marker the frontend
// matches to route a first-login admin into the forced password change flow.
const passwordChangeRequiredMessage = "password_change_required"

// passwordChangeExemptPath lists the only endpoints reachable while a
// system-generated temporary password is still in place: identity, the
// password change itself, reauth (the change flow may need it), and logout.
// Every other admin capability stays locked until the password is rotated.
func passwordChangeExemptPath(path string) bool {
	switch path {
	case "/api/admin/me", "/api/admin/logout", "/api/admin/reauth", "/api/admin/security/password":
		return true
	default:
		return false
	}
}

func CurrentAdmin(ctx context.Context) (Admin, bool) {
	account, ok := ctx.Value(contextKey{}).(Admin)
	return account, ok
}

// ContextWithAdmin returns a context carrying the authenticated admin, mirroring
// what RequireAuthentication injects. Production auth still flows only through
// RequireAuthentication; this exists so handlers in other packages can be unit
// tested against a known admin without a database or a real session.
func ContextWithAdmin(ctx context.Context, account Admin) context.Context {
	return context.WithValue(ctx, contextKey{}, account)
}
