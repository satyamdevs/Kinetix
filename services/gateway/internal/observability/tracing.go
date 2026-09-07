package observability

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const defaultServiceName = "orca-gateway"

// Tracer owns the OpenTelemetry SDK tracer provider. A nil Tracer is a no-op
// and must never fail or observe a request.
type Tracer struct {
	tp *sdktrace.TracerProvider
}

// TracingConfig configures the OTLP trace exporter.
type TracingConfig struct {
	// Endpoint is the OTLP HTTP endpoint (e.g. http://localhost:4318). Leave
	// empty to disable trace export entirely (no-op provider, ~zero overhead).
	Endpoint    string
	ServiceName string
	Environment string
}

// SetupTracing initializes the tracer provider that exports spans over OTLP
// HTTP. When Endpoint is empty it returns a nil Tracer and a no-op shutdown;
// tracing is best-effort and never blocks startup or requests.
func SetupTracing(cfg TracingConfig) (*Tracer, func(context.Context) error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, func(context.Context) error { return nil }
	}

	exp, err := otlptracehttp.New(context.Background(), otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		slog.Warn("otlp exporter unavailable; tracing disabled", "err", err)
		return nil, func(context.Context) error { return nil }
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", orStr(cfg.ServiceName, defaultServiceName)),
			attribute.String("deployment.environment", orStr(cfg.Environment, "dev")),
		)),
	)
	otel.SetTracerProvider(tp)

	return &Tracer{tp: tp}, func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), exp.Shutdown(ctx))
	}
}

// Transport wraps base for outbound HTTP with W3C trace context propagation
// and per-call client spans. A nil Tracer returns base unchanged.
func (t *Tracer) Transport(base http.RoundTripper) http.RoundTripper {
	if t == nil || t.tp == nil {
		return base
	}
	return otelhttp.NewTransport(base, otelhttp.WithTracerProvider(t.tp))
}

// TracingMiddleware instruments the server with an OpenTelemetry span per
// request and stamps an X-Trace-Id response header so callers can correlate.
// A nil Tracer is a transparent pass-through.
func TracingMiddleware(t *Tracer, service string) func(http.Handler) http.Handler {
	if t == nil || t.tp == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	name := orStr(service, defaultServiceName)
	decorate := otelhttp.NewMiddleware(name,
		otelhttp.WithTracerProvider(t.tp),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return "HTTP " + r.Method + " " + r.URL.Path
		}),
	)
	return func(next http.Handler) http.Handler {
		return decorate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tid := TraceID(r.Context()); tid != "" {
				w.Header().Set("X-Trace-Id", tid)
			}
			next.ServeHTTP(w, r)
		}))
	}
}

// TraceID returns the hex trace id of the active span in ctx, or "" when the
// context carries no valid (sampled) span.
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || !sc.IsSampled() {
		return ""
	}
	return sc.TraceID().String()
}

func orStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
