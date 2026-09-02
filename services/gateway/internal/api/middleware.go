package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"orca/gateway/internal/keys"
)

const bearerPrefix = "Bearer "

type keyContextKey struct{}

// Authenticate validates the bearer token against the key store and attaches
// the resolved governance record to the request context.
func Authenticate(store keys.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := bearerKey(r)
			if !ok {
				writeError(w, http.StatusUnauthorized,
					"Missing API key. Provide it via 'Authorization: Bearer <key>'.",
					"invalid_request_error", "missing_api_key")
				return
			}

			k, err := store.Lookup(r.Context(), keys.Hash(raw))
			if err != nil {
				writeError(w, http.StatusUnauthorized,
					"Incorrect API key provided.",
					"invalid_request_error", "invalid_api_key")
				return
			}
			if err := keys.Validate(k); err != nil {
				writeError(w, http.StatusUnauthorized,
					apikeyRejectedMessage(err),
					"invalid_request_error", "invalid_api_key")
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyContextKey{}, k)))
		})
	}
}

func bearerKey(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, bearerPrefix) {
		return "", false
	}
	key := strings.TrimSpace(strings.TrimPrefix(header, bearerPrefix))
	return key, key != ""
}

func KeyFromContext(ctx context.Context) *keys.Key {
	k, _ := ctx.Value(keyContextKey{}).(*keys.Key)
	return k
}

func apikeyRejectedMessage(err error) string {
	switch {
	case errors.Is(err, keys.ErrDisabled):
		return "API key is disabled."
	case errors.Is(err, keys.ErrExpired):
		return "API key has expired."
	default:
		return "Incorrect API key provided."
	}
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
