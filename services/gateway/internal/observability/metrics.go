package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"orca/gateway/internal/telemetry"
)

// Rejection categories recorded on orca_rejected_requests_total.
const (
	RejectedMissingKey   = "missing_api_key"
	RejectedInvalidKey   = "invalid_api_key"
	RejectedRateLimit    = "rate_limited"
	RejectedQuota        = "quota_exceeded"
	RejectedGuardrail    = "guardrail"
	RejectedInvalidBody  = "invalid_body"
	RejectedMissingModel = "missing_model"
	RejectedUnknownModel = "unknown_model"
	RejectedUpstream     = "upstream_error"
)

// The latency buckets cover streaming completions (which can legitimately
// take many seconds) as well as fast L1/L2 cache-fast proxy round trips.
var durationBuckets = []float64{
	0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1,
	0.25, 0.5, 1, 2.5, 5, 10, 30, 60,
}

// Metrics owns the Prometheus registry and collectors for gateway
// observability. All methods are nil-safe so an unconfigured Governance
// degrades to no-op behavior in tests and dev.
type Metrics struct {
	reg              *prometheus.Registry
	httpRequests     *prometheus.CounterVec
	httpDuration     *prometheus.HistogramVec
	completions      *prometheus.CounterVec
	tokens           *prometheus.CounterVec
	cost             *prometheus.CounterVec
	rejected         *prometheus.CounterVec
	upstreamRequests *prometheus.CounterVec
}

// NewMetrics builds a metrics registry with the Orca collectors registered.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	f := promauto.With(reg)

	return &Metrics{
		reg: reg,
		httpRequests: f.NewCounterVec(prometheus.CounterOpts{
			Name: "orca_http_requests_total",
			Help: "HTTP requests handled by the gateway, by route and status.",
		}, []string{"route", "method", "status"}),
		httpDuration: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "orca_http_request_duration_seconds",
			Help:    "Latency of gateway HTTP requests observed at the router.",
			Buckets: durationBuckets,
		}, []string{"route"}),
		completions: f.NewCounterVec(prometheus.CounterOpts{
			Name: "orca_completions_total",
			Help: "Completed chat requests by model, provider, status and key.",
		}, []string{"model", "provider", "status", "key_id"}),
		tokens: f.NewCounterVec(prometheus.CounterOpts{
			Name: "orca_tokens_total",
			Help: "Prompt and completion tokens accounted by the gateway.",
		}, []string{"model", "direction"}),
		cost: f.NewCounterVec(prometheus.CounterOpts{
			Name: "orca_cost_usd_total",
			Help: "Accumulated USD cost by model and provider.",
		}, []string{"model", "provider"}),
		rejected: f.NewCounterVec(prometheus.CounterOpts{
			Name: "orca_rejected_requests_total",
			Help: "Requests rejected by governance or validation, by reason.",
		}, []string{"reason"}),
		upstreamRequests: f.NewCounterVec(prometheus.CounterOpts{
			Name: "orca_upstream_requests_total",
			Help: "Requests forwarded to upstream providers, by outcome.",
		}, []string{"provider", "model", "status"}),
	}
}

// ObserveHTTP records a completed HTTP request for the metrics middleware.
func (m *Metrics) ObserveHTTP(route, method string, status int, d time.Duration) {
	if m == nil || m.httpRequests == nil {
		return
	}
	m.httpRequests.WithLabelValues(route, method, strconv.Itoa(status)).Inc()
	m.httpDuration.WithLabelValues(route).Observe(d.Seconds())
}

// ObserveCompletion records a finished chat completion: counts, tokens, and
// cost. Called by the completion telemetry path.
func (m *Metrics) ObserveCompletion(rec telemetry.Record) {
	if m == nil || m.completions == nil {
		return
	}
	m.completions.WithLabelValues(rec.Model, rec.Provider, strconv.Itoa(rec.Status), rec.APIKeyID).Inc()
	m.tokens.WithLabelValues(rec.Model, "prompt").Add(float64(rec.Usage.PromptTokens))
	m.tokens.WithLabelValues(rec.Model, "completion").Add(float64(rec.Usage.CompletionTokens))
	m.cost.WithLabelValues(rec.Model, rec.Provider).Add(rec.CostUSD)
}

// ObserveUpstream records the outcome of a forwarded upstream call.
func (m *Metrics) ObserveUpstream(provider, model string, ok bool) {
	if m == nil || m.upstreamRequests == nil {
		return
	}
	status := "ok"
	if !ok {
		status = "error"
	}
	m.upstreamRequests.WithLabelValues(provider, model, status).Inc()
}

// Rejected increments the rejection counter for a governance reason.
func (m *Metrics) Rejected(reason string) {
	if m == nil || m.rejected == nil {
		return
	}
	m.rejected.WithLabelValues(reason).Inc()
}

// Handler returns the Prometheus scrape handler for /metrics.
func (m *Metrics) Handler() http.Handler {
	if m == nil || m.reg == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	}
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// MetricsMiddleware observes every request at the router root, capturing the
// matched route pattern and final status after downstream middleware writes it.
func MetricsMiddleware(m *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			route := ""
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				route = rctx.RoutePattern()
			}
			m.ObserveHTTP(route, r.Method, ww.Status(), time.Since(start))
		})
	}
}
