package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"orca/gateway/internal/api"
	"orca/gateway/internal/keys"
	"orca/gateway/internal/limits"
	"orca/gateway/internal/provider"
)

// benchProvider returns instantly, so any measured latency is the gateway's own
// proxy/routing/governance overhead rather than the backing provider.
type benchProvider struct{}

func (benchProvider) Name() string { return "bench" }

func (benchProvider) Complete(context.Context, *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return &provider.CompletionResponse{
		ID: "bench", Model: "bench",
		Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "ok"}}},
		Usage:   provider.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
	}, nil
}

func (benchProvider) Stream(_ context.Context, _ *provider.CompletionRequest, emit func(provider.StreamChunk) error) error {
	if err := emit(provider.StreamChunk{ID: "bench", Model: "bench", Delta: "ok"}); err != nil {
		return err
	}
	return emit(provider.StreamChunk{Finish: "stop", Usage: &provider.Usage{TotalTokens: 7}})
}

// benchRouter wires the full governance pipeline (auth + rate limit + quota)
// around the instant provider, mirroring production routing.
func benchRouter() http.Handler {
	mem := keys.NewMemory()
	mem.Upsert(&keys.Key{
		ID: "b", Name: "b", Owner: "b", Hash: keys.Hash("sk-bench"),
		Enabled: true, Limit: keys.Limit{RequestsPerMinute: 0, TokensPerMonth: 0},
	})
	gov := &api.Governance{Keys: mem, Limiter: limits.NewMemory()}
	r := provider.NewRegistry()
	r.Register("bench", benchProvider{})
	return api.NewRouter(gov, r)
}

// BenchmarkOverhead measures the request-handling overhead added by the Orca
// gateway itself for an already-authenticated non-streamed completion. The
// guard is the README's Phase 6 target: under 10ms p50 gateway overhead.
func BenchmarkOverheadNonStream(b *testing.B) {
	h := benchRouter()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := strings.NewReader(`{"model":"bench","messages":[{"role":"user","content":"hi"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
		req.Header.Set("Authorization", "Bearer sk-bench")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d", rec.Code)
		}
	}
}

func BenchmarkOverheadStream(b *testing.B) {
	h := benchRouter()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := strings.NewReader(`{"model":"bench","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
		req.Header.Set("Authorization", "Bearer sk-bench")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d", rec.Code)
		}
	}
}
