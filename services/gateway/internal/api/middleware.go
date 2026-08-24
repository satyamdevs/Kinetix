package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

func RequireBearerKey(apiKeys []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(apiKeys))
	for _, k := range apiKeys {
		allowed[k] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")

			if !strings.HasPrefix(header, bearerPrefix) {
				writeError(w, http.StatusUnauthorized,
					"Missing API key. Provide it via 'Authorization: Bearer <key>'.",
					"invalid_request_error", "missing_api_key")
				return
			}

			key := strings.TrimSpace(strings.TrimPrefix(header, bearerPrefix))
			if _, ok := allowed[key]; !ok {
				writeError(w, http.StatusUnauthorized,
					"Incorrect API key provided.",
					"invalid_request_error", "invalid_api_key")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
