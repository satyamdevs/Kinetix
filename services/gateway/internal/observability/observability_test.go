package observability

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"orca/gateway/internal/telemetry"
)

func TestMetricsNilSafety(t *testing.T) {
	var m *Metrics
	m.ObserveHTTP("/x", http.MethodGet, http.StatusOK, time.Millisecond)
	m.ObserveCompletion(telemetry.Record{})
	m.ObserveUpstream("openai", "gpt-4o", false)
	m.Rejected(RejectedRateLimit)
	resp := httptest.NewRecorder()
	m.Handler().ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("nil metrics handler status = %d", resp.Code)
	}
}

func TestMetricsHandler(t *testing.T) {
	m := NewMetrics()
	m.ObserveHTTP("/v1/chat/completions", http.MethodPost, http.StatusOK, 2*time.Millisecond)
	m.ObserveCompletion(telemetry.Record{
		Model: "gpt-4o-mini", Provider: "openai", Status: 200, APIKeyID: "k1",
		Usage: telemetry.Usage{PromptTokens: 5, CompletionTokens: 7}, CostUSD: 0.001,
	})
	m.ObserveUpstream("openai", "gpt-4o-mini", true)
	m.Rejected(RejectedRateLimit)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	for _, want := range []string{
		"orca_http_requests_total",
		"orca_http_request_duration_seconds",
		"orca_completions_total",
		"orca_tokens_total",
		"orca_cost_usd_total",
		"orca_rejected_requests_total",
		"orca_upstream_requests_total",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestMetricsMiddleware(t *testing.T) {
	m := NewMetrics()
	r := chi.NewRouter()
	r.Use(MetricsMiddleware(m))
	r.Post("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	rec := httptest.NewRecorder()
	h := r
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	if got := rec.Code; got != http.StatusTooManyRequests {
		t.Fatalf("status = %d", got)
	}
	scrape := httptest.NewRecorder()
	m.Handler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(scrape.Body.String(), `orca_http_requests_total{method="POST",route="/v1/chat/completions",status="429"} 1`) {
		t.Fatalf("expected observed counter, got:\n%s", scrape.Body.String())
	}
}

func TestTraceIDEmptyWithoutSpan(t *testing.T) {
	if got := TraceID(context.Background()); got != "" {
		t.Fatalf("TraceID(background) = %q, want empty", got)
	}
}

func TestTraceIDReturnsSampledSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(context.Background())
	tr := tp.Tracer("test")
	ctx, span := tr.Start(context.Background(), "span")
	defer span.End()

	tid := TraceID(ctx)
	if tid == "" {
		t.Fatal("TraceID empty for active sampled span")
	}
	sc := trace.SpanContextFromContext(ctx)
	if !strings.HasPrefix(tid, sc.TraceID().String()[:8]) {
		t.Fatalf("TraceID %q does not match span context %q", tid, sc.TraceID())
	}
}

func TestTracingMiddlewarePassThroughWhenNil(t *testing.T) {
	called := false
	h := TracingMiddleware(nil, "orca.gateway")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !called || rec.Code != http.StatusOK {
		t.Fatalf("nil tracer middleware should pass through, called=%v code=%d", called, rec.Code)
	}
}
