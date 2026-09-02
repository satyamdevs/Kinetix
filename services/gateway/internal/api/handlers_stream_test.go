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

func testRouter(r *provider.Registry) http.Handler {
	mem := keys.NewMemory()
	mem.Upsert(&keys.Key{ID: "test", Name: "test", Owner: "test", Hash: keys.Hash("sk-test"), Enabled: true})
	gov := &api.Governance{Keys: mem, Limiter: limits.NewMemory()}
	return api.NewRouter(gov, r)
}

type fakeStreamer struct{}

func (fakeStreamer) Name() string { return "fake" }

func (fakeStreamer) Complete(context.Context, *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return &provider.CompletionResponse{ID: "fake-id", Model: "fake", Choices: []provider.Choice{{
		Message:      provider.Message{Role: "assistant", Content: "hi"},
		FinishReason: "stop",
	}}, Usage: provider.Usage{PromptTokens: 8, CompletionTokens: 1, TotalTokens: 9}}, nil
}

func (fakeStreamer) Stream(_ context.Context, _ *provider.CompletionRequest, emit func(provider.StreamChunk) error) error {
	if err := emit(provider.StreamChunk{ID: "fake-id", Model: "fake", Delta: "hello"}); err != nil {
		return err
	}
	return emit(provider.StreamChunk{
		Finish: "stop",
		Usage:  &provider.Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8},
	})
}

func TestStreamChatCompletionsSSE(t *testing.T) {
	r := provider.NewRegistry()
	r.Register("fake", fakeStreamer{})
	router := testRouter(r)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"fake","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"data: ",
		`"object":"chat.completion.chunk"`,
		`"role":"assistant"`,
		`"content":"hello"`,
		`"finish_reason":"stop"`,
		`"total_tokens":8`,
		"data: [DONE]",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("SSE body missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestChatCompletionsNonStream(t *testing.T) {
	r := provider.NewRegistry()
	r.Register("fake", fakeStreamer{})
	router := testRouter(r)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"fake","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"object":"chat.completion"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}
