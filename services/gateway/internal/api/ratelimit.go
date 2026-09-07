package api

import (
	"errors"
	"fmt"
	"net/http"

	"orca/gateway/internal/limits"
	"orca/gateway/internal/observability"
)

// RateLimit enforces the per-key requests-per-minute cap using the shared
// limiter. Keys with no configured cap and values under the configured policy
// fall back to the gateway-wide policy.
func RateLimit(l limits.Limiter, policy limits.Policy, m *observability.Metrics) func(http.Handler) http.Handler {
	if l == nil {
		return passthrough()
	}
	window := policy.Window
	if window <= 0 {
		window = limits.DefaultWindow
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := KeyFromContext(r.Context())
			if k == nil {
				next.ServeHTTP(w, r)
				return
			}

			rpm := k.Limit.RequestsPerMinute
			if rpm <= 0 {
				rpm = policy.RequestsPerMinute
			}

			dec, err := l.Allow(r.Context(), k.ID, rpm, window)
			if err != nil {
				writeError(w, http.StatusInternalServerError,
					"Rate limiter unavailable: "+err.Error(),
					"api_error", "rate_limiter_error")
				return
			}
			w.Header().Set("X-RateLimit-Limit", fmt.Sprint(rpm))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprint(max(dec.Remaining, 0)))
			if !dec.OK {
				m.Rejected(observability.RejectedRateLimit)
				writeError(w, http.StatusTooManyRequests,
					"You have exceeded the allowed request rate. Retry later.",
					"rate_limit_error", "rate_limit_exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Quota blocks requests for keys that have already exhausted their monthly
// token budget. Actual token spend is recorded after each completion by the
// handlers.
func Quota(l limits.Limiter, m *observability.Metrics) func(http.Handler) http.Handler {
	if l == nil {
		return passthrough()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := KeyFromContext(r.Context())
			if k == nil || k.Limit.TokensPerMonth <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			spent, err := l.Tokens(r.Context(), k.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError,
					"Quota store unavailable: "+err.Error(),
					"api_error", "quota_error")
				return
			}
			if spent >= k.Limit.TokensPerMonth {
				m.Rejected(observability.RejectedQuota)
				writeError(w, http.StatusTooManyRequests,
					"You have exceeded your monthly token quota.",
					"quota_error", "quota_exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// spendQuota records actual usage after a completion. Errors are logged by the
// caller; they never fail an already-served request.
func spendQuota(r *http.Request, l limits.Limiter, used int64) error {
	if l == nil || used <= 0 {
		return nil
	}
	k := KeyFromContext(r.Context())
	if k == nil {
		return nil
	}
	dec, err := l.AddTokens(r.Context(), k.ID, used, k.Limit.TokensPerMonth)
	if err != nil {
		return err
	}
	if !dec.OK {
		return errors.New("monthly token quota exceeded")
	}
	return nil
}

func passthrough() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
