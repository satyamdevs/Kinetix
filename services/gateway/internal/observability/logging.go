package observability

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// RequestLoggingMiddleware emits one structured JSON log line per HTTP request
// with request_id, trace_id, status, and duration. Levels: info for 2xx/3xx,
// warn for 4xx, error for 5xx.
func RequestLoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			level := slog.LevelInfo
			switch {
			case ww.Status() >= 500:
				level = slog.LevelError
			case ww.Status() >= 400:
				level = slog.LevelWarn
			}
			logger.Log(r.Context(), level, "http request",
				"request_id", middleware.GetReqID(r.Context()),
				"trace_id", TraceID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", float64(time.Since(start).Microseconds())/1000.0,
				"remote", r.RemoteAddr,
			)
		})
	}
}
