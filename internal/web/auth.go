package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"nir/internal/storage"
)

type contextKey string

const ctxUserKey contextKey = "user"

const sessionCookieName = "sid"
const sessionTTL = 24 * time.Hour

// currentUser extracts the authenticated user from the request context.
func currentUser(r *http.Request) *storage.UserRow {
	u, _ := r.Context().Value(ctxUserKey).(*storage.UserRow)
	return u
}

// generateToken creates a cryptographically random 32-byte hex token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// authRequired middleware: validates session cookie, injects user into context.
func (h *Handler) authRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, 401, "not authenticated")
			return
		}

		sess, err := h.store.GetSession(r.Context(), cookie.Value)
		if err != nil || sess.ExpiresAt.Before(time.Now()) {
			http.SetCookie(w, &http.Cookie{Name: sessionCookieName, MaxAge: -1, Path: "/"})
			writeError(w, 401, "session expired")
			return
		}

		user, err := h.store.GetUserByID(r.Context(), sess.UserID)
		if err != nil {
			writeError(w, 401, "user not found")
			return
		}

		ctx := context.WithValue(r.Context(), ctxUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireRole middleware: only allows users with one of the given roles.
// Must be chained after authRequired.
func requireRole(roles ...string) func(http.Handler) http.Handler {
	set := make(map[string]bool, len(roles))
	for _, r := range roles {
		set[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := currentUser(r)
			if u == nil || !set[u.Role] {
				writeError(w, 403, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
