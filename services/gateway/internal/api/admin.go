package api

import (
	"net/http"
	"strconv"
	"time"

	"orca/gateway/internal/telemetry"
)

const adminScope = "admin"

// adminOnly restricts an admin route to keys carrying the "admin" scope.
func adminOnly(gov *Governance) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := KeyFromContext(r.Context())
			if k == nil || !hasScope(k.Scopes, adminScope) {
				writeError(w, http.StatusForbidden,
					"Admin scope required for this endpoint.",
					"invalid_request_error", "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

// handleUsageSummary returns aggregated usage over a range, optionally split
// per model. Query params: from (ISO time), to (ISO time), model.
func handleUsageSummary(q telemetry.Querier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if q == nil {
			writeError(w, http.StatusServiceUnavailable,
				"Telemetry query store is not configured.",
				"api_error", "telemetry_unconfigured")
			return
		}

		rng, err := parseRange(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_range")
			return
		}

		sum, err := q.UsageSummary(r.Context(), rng)
		if err != nil {
			writeError(w, http.StatusInternalServerError,
				"Usage summary query failed: "+err.Error(),
				"api_error", "query_failed")
			return
		}
		writeJSON(w, http.StatusOK, sum)
	}
}

func parseRange(r *http.Request) (telemetry.Range, error) {
	to := time.Now().UTC()
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := parseTime(v)
		if err != nil {
			return telemetry.Range{}, err
		}
		to = t
	}
	from := to.AddDate(0, 0, -7)
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := parseTime(v)
		if err != nil {
			return telemetry.Range{}, err
		}
		from = t
	}
	return telemetry.Range{From: from, To: to, Model: r.URL.Query().Get("model")}, nil
}

func parseTime(v string) (time.Time, error) {
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Unix(n, 0).UTC(), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, &badRangeError{Message: "invalid time, use RFC3339, YYYY-MM-DD, or a Unix timestamp"}
}

type badRangeError struct{ Message string }

func (e *badRangeError) Error() string { return e.Message }
