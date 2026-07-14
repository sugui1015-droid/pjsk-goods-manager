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

		ctx := context.WithValue(r.Context(), contextKey{}, account)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func CurrentAdmin(ctx context.Context) (Admin, bool) {
	account, ok := ctx.Value(contextKey{}).(Admin)
	return account, ok
}
