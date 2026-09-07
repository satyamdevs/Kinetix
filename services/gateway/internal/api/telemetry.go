package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"orca/gateway/internal/observability"
	"orca/gateway/internal/provider"
	"orca/gateway/internal/telemetry"
)

func telemetryOf(gov *Governance) telemetry.Sink {
	if gov == nil {
		return nil
	}
	return gov.Telemetry
}

func pricerOf(gov *Governance) *telemetry.Pricer {
	if gov == nil || gov.Pricer == nil {
		return telemetry.DefaultPricer()
	}
	return gov.Pricer
}

// recordCompletion writes a telemetry record after a request completes. It is
// best-effort: writes happen on a separate goroutine and failures are logged,
// never propagated to the caller. It also feeds the observability metrics.
func recordCompletion(ctx context.Context, gov *Governance, status int, streamed bool,
	model, provider string, usage provider.Usage, latency time.Duration) {

	rec := telemetry.Record{
		RequestID: requestID(ctx),
		APIKeyID:  keyID(ctx),
		Model:     model,
		Provider:  provider,
		Streamed:  streamed,
		Status:    status,
		TraceID:   observability.TraceID(ctx),
		Usage: telemetry.Usage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
		},
		CostUSD:    pricerOf(gov).Cost(model, usage.PromptTokens, usage.CompletionTokens),
		Latency:    latency,
		FinishedAt: time.Now().UTC(),
	}

	if m := metricsOf(gov); m != nil {
		m.ObserveCompletion(rec)
		m.ObserveUpstream(provider, model, status == http.StatusOK)
	}

	sink := telemetryOf(gov)
	if sink == nil {
		return
	}

	ctx = context.WithoutCancel(ctx)
	go func() {
		_ = sink.Record(ctx, rec)
	}()
}

func requestID(ctx context.Context) string {
	if v, ok := ctx.Value(middleware.RequestIDKey).(string); ok {
		return v
	}
	return ""
}

func keyID(ctx context.Context) string {
	if k := KeyFromContext(ctx); k != nil {
		return k.ID
	}
	return ""
}
