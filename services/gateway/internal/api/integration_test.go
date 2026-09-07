package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"orca/gateway/internal/api"
	"orca/gateway/internal/keys"
	"orca/gateway/internal/limits"
	"orca/gateway/internal/provider"
)

// failingThenOK is a provider that fails its first N calls for a given model
// and then succeeds, modeling a dead upstream that a fallback must step in for.
type failingThenOK struct {
	model string
	done  bool
}

func (f *failingThenOK) Name() string { return "primary-" + f.model }

func (f *failingThenOK) Complete(context.Context, *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	if !f.done {
		f.done = true
		return nil, errors.New("upstream timeout")
	}
	return &provider.CompletionResponse{
		ID: f.model + "-ok", Model: f.model,
		Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "recovered"}}},
		Usage:   provider.Usage{TotalTokens: 4},
	}, nil
}

// integrationRouter wires governance + a registry with a fallback alias whose
// primary is down, so requests must route through the secondary.
func integrationRouter(primary, secondary provider.Provider) http.Handler {
	mem := keys.NewMemory()
	mem.Upsert(&keys.Key{ID: "it", Name: "it", Owner: "it", Hash: keys.Hash("sk-int"), Enabled: true})
	gov := &api.Governance{Keys: mem, Limiter: limits.NewMemory()}
	r := provider.NewRegistry()
	r.RegisterWithFallbacks("alias", primary, secondary)
	return api.NewRouter(gov, r)
}

func TestIntegrationFailoverHTTPRoutesToHealthyBackend(t *testing.T) {
	primary := &failingThenOK{model: "primary"}
	secondary := &stubProviderAdapter{name: "secondary", content: "from fallback"}
	h := integrationRouter(primary, secondary)

	res := postCompletion(h, `{"model":"alias","messages":[{"role":"user","content":"hi"}]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, res.Body.String())
	}
	if len(out.Choices) == 0 || out.Choices[0].Message.Content != "from fallback" {
		t.Fatalf("expected fallback content, got %+v", out.Choices)
	}
}

func TestIntegrationAllUpstreamsDownReturns502(t *testing.T) {
	primary := &alwaysFail{name: "one"}
	secondary := &alwaysFail{name: "two"}
	h := integrationRouter(primary, secondary)

	res := postCompletion(h, `{"model":"alias","messages":[{"role":"user","content":"hi"}]}`)
	if res.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body=%s)", res.Code, res.Body.String())
	}
}

// alwaysFail is a provider that fails every call, modeling every upstream being down.
type alwaysFail struct{ name string }

func (f *alwaysFail) Name() string { return "down-" + f.name }

func (f *alwaysFail) Complete(context.Context, *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, errors.New("upstream timeout " + f.name)
}

func (f *alwaysFail) Stream(context.Context, *provider.CompletionRequest, func(provider.StreamChunk) error) error {
	return errors.New("upstream timeout " + f.name)
}

// stubProviderAdapter is a minimal streaming+non-streaming provider that
// returns fixed content, distinct from the package-internal fakeStreamer.
type stubProviderAdapter struct {
	name    string
	content string
}

func (s *stubProviderAdapter) Name() string { return s.name }

func (s *stubProviderAdapter) Complete(context.Context, *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return &provider.CompletionResponse{
		ID: s.name, Model: s.name,
		Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: s.content}}},
		Usage:   provider.Usage{TotalTokens: 5},
	}, nil
}

func (s *stubProviderAdapter) Stream(_ context.Context, _ *provider.CompletionRequest, emit func(provider.StreamChunk) error) error {
	if err := emit(provider.StreamChunk{ID: s.name, Model: s.name, Delta: s.content}); err != nil {
		return err
	}
	return emit(provider.StreamChunk{Finish: "stop"})
}

func postCompletion(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-int")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
