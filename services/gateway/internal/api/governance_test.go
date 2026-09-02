package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"orca/gateway/internal/api"
	"orca/gateway/internal/guardrails"
	"orca/gateway/internal/keys"
	"orca/gateway/internal/limits"
	"orca/gateway/internal/provider"
)

type quotaProvider struct{ tokens int64 }

func (quotaProvider) Name() string { return "quota" }

func (p quotaProvider) Complete(_ context.Context, _ *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return &provider.CompletionResponse{
		ID:    "q",
		Model: "q",
		Usage: provider.Usage{TotalTokens: int(p.tokens)},
		Choices: []provider.Choice{
			{Message: provider.Message{Role: "assistant", Content: "fine"}},
		},
	}, nil
}

type badOutputProvider struct{}

func (badOutputProvider) Name() string { return "bad" }

func (badOutputProvider) Complete(_ context.Context, _ *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return &provider.CompletionResponse{
		ID: "b", Model: "b",
		Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "this is a violent suggestion"}}},
	}, nil
}

func governanceHandler(k *keys.Key, l limits.Limiter, g *guardrails.Filter, model string, p provider.Provider) http.Handler {
	mem := keys.NewMemory()
	mem.Upsert(k)
	gov := &api.Governance{Keys: mem, Limiter: l, Guardrails: g}
	r := provider.NewRegistry()
	r.Register(model, p)
	return api.NewRouter(gov, r)
}

func doAuthReq(t *testing.T, h http.Handler, method, path, body, auth string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthValidAndRejectedKeys(t *testing.T) {
	raw, k, _ := keys.Generate("ci", "tester", keys.Limit{}, 0)
	h := governanceHandler(k, limits.NewMemory(), nil, "fake", fakeStreamer{})

	if rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", "Bearer "+raw); rec.Code != http.StatusOK {
		t.Fatalf("valid key: status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key: status = %d", rec.Code)
	}
	if rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", "Bearer sk-orca-wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown key: status = %d", rec.Code)
	}
	if rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", "Basic dXNlcjpwYXNz"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("non-bearer auth: status = %d", rec.Code)
	}
}

func TestAuthDisabledAndExpiredKeys(t *testing.T) {
	raw, disabled, _ := keys.Generate("d", "d", keys.Limit{}, 0)
	disabled.Enabled = false

	h := governanceHandler(disabled, limits.NewMemory(), nil, "fake", fakeStreamer{})
	rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", "Bearer "+raw)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("disabled key: status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "disabled") {
		t.Fatalf("disabled key should be reported: %s", rec.Body.String())
	}

	expiredRaw, expired, _ := keys.Generate("e", "e", keys.Limit{}, 0)
	past := time.Now().Add(-time.Hour)
	expired.ExpiresAt = &past
	h2 := governanceHandler(expired, limits.NewMemory(), nil, "fake", fakeStreamer{})
	rec = doAuthReq(t, h2, http.MethodGet, "/v1/models", "", "Bearer "+expiredRaw)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired key: status = %d, want 401", rec.Code)
	}
}

func TestRateLimitBlocked(t *testing.T) {
	raw, k, _ := keys.Generate("rl", "t", keys.Limit{RequestsPerMinute: 1}, 0)
	h := governanceHandler(k, limits.NewMemory(), nil, "fake", fakeStreamer{})
	auth := "Bearer " + raw

	if rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", auth); rec.Code != http.StatusOK {
		t.Fatalf("first request: status = %d", rec.Code)
	}
	if rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", auth); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaBlocked(t *testing.T) {
	l := limits.NewMemory()
	raw, k, _ := keys.Generate("quota", "t", keys.Limit{TokensPerMonth: 10}, 0)
	if _, err := l.AddTokens(context.Background(), k.ID, 10, 10); err != nil {
		t.Fatal(err)
	}
	h := governanceHandler(k, l, nil, "fake", fakeStreamer{})
	if rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", "Bearer "+raw); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-quota key: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestQuotaAccounting(t *testing.T) {
	l := limits.NewMemory()
	raw, k, _ := keys.Generate("spend", "t", keys.Limit{TokensPerMonth: 500}, 0)
	h := governanceHandler(k, l, nil, "q", quotaProvider{tokens: 50})

	rec := doAuthReq(t, h, http.MethodPost, "/v1/chat/completions",
		`{"model":"q","messages":[{"role":"user","content":"hi"}]}`, "Bearer "+raw)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	spent, err := l.Tokens(context.Background(), k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if spent != 50 {
		t.Fatalf("spent = %d, want 50", spent)
	}
}

func TestGuardrailBlocksInput(t *testing.T) {
	rule, _ := guardrails.DenyKeyword("block", "banned-word", "not allowed", guardrails.RoleInput)
	raw, k, _ := keys.Generate("gr", "t", keys.Limit{}, 0)
	h := governanceHandler(k, limits.NewMemory(), guardrails.New(rule), "fake", fakeStreamer{})
	auth := "Bearer " + raw

	rec := doAuthReq(t, h, http.MethodPost, "/v1/chat/completions",
		`{"model":"fake","messages":[{"role":"user","content":"contains banned-word here"}]}`, auth)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "content_policy_violation") {
		t.Fatalf("missing violation payload: %s", rec.Body.String())
	}

	rec = doAuthReq(t, h, http.MethodPost, "/v1/chat/completions",
		`{"model":"fake","messages":[{"role":"user","content":"all clear"}]}`, auth)
	if rec.Code != http.StatusOK {
		t.Fatalf("clean request: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGuardrailBlocksOutput(t *testing.T) {
	rule, _ := guardrails.DenyKeyword("block", "violent", "blocked", guardrails.RoleOutput)
	raw, k, _ := keys.Generate("out", "t", keys.Limit{}, 0)
	h := governanceHandler(k, limits.NewMemory(), guardrails.New(rule), "bad", badOutputProvider{})

	rec := doAuthReq(t, h, http.MethodPost, "/v1/chat/completions",
		`{"model":"bad","messages":[{"role":"user","content":"hi"}]}`, "Bearer "+raw)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestModelsListNeedsAuth(t *testing.T) {
	raw, k, _ := keys.Generate("m", "t", keys.Limit{}, 0)
	h := governanceHandler(k, limits.NewMemory(), nil, "fake", fakeStreamer{})

	if rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated models: status = %d", rec.Code)
	}
	if rec := doAuthReq(t, h, http.MethodGet, "/v1/models", "", "Bearer "+raw); rec.Code != http.StatusOK {
		t.Fatalf("authenticated models: status = %d", rec.Code)
	}
}

func TestNoKeyStoreRejectsRequest(t *testing.T) {
	r := provider.NewRegistry()
	r.Register("fake", fakeStreamer{})
	router := api.NewRouter(&api.Governance{Limiter: limits.NewMemory()}, r)

	rec := doAuthReq(t, router, http.MethodGet, "/v1/models", "", "Bearer anything")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
