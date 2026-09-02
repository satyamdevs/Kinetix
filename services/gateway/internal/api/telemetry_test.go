package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"orca/gateway/internal/api"
	"orca/gateway/internal/keys"
	"orca/gateway/internal/limits"
	"orca/gateway/internal/provider"
	"orca/gateway/internal/telemetry"
)

func TestTelemetryRecordedNonStream(t *testing.T) {
	raw, k, _ := keys.Generate("tel", "t", keys.Limit{}, 0)
	buf := telemetry.NewBuffer(8)

	mem := keys.NewMemory()
	mem.Upsert(k)
	gov := &api.Governance{
		Keys:      mem,
		Limiter:   limits.NewMemory(),
		Telemetry: buf,
		Pricer:    telemetry.DefaultPricer(),
	}
	r := provider.NewRegistry()
	r.Register("fake", fakeStreamer{})
	router := api.NewRouter(gov, r)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"fake","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Wait for the async write to land on the buffer.
	deadline := time.Now().Add(2 * time.Second)
	for buf.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if buf.Len() == 0 {
		t.Fatal("expected a telemetry record to be recorded")
	}
	snap := buf.Snapshot()
	rec0 := snap[0]
	if rec0.Model != "fake" {
		t.Fatalf("model = %q, want fake", rec0.Model)
	}
	if rec0.APIKeyID != k.ID {
		t.Fatalf("key = %q, want %q", rec0.APIKeyID, k.ID)
	}
	if rec0.Streamed {
		t.Fatal("non-stream request flagged as streamed")
	}
	if rec0.Usage.TotalTokens != 9 {
		t.Fatalf("total_tokens = %d, want 9", rec0.Usage.TotalTokens)
	}
}

func TestTelemetryRecordedStream(t *testing.T) {
	raw, k, _ := keys.Generate("tel", "t", keys.Limit{}, 0)
	buf := telemetry.NewBuffer(8)

	mem := keys.NewMemory()
	mem.Upsert(k)
	gov := &api.Governance{
		Keys:      mem,
		Limiter:   limits.NewMemory(),
		Telemetry: buf,
		Pricer:    telemetry.DefaultPricer(),
	}
	r := provider.NewRegistry()
	r.Register("fake", fakeStreamer{})
	router := api.NewRouter(gov, r)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"fake","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for buf.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if buf.Len() == 0 {
		t.Fatal("expected a telemetry record to be recorded")
	}
	snap := buf.Snapshot()
	rec0 := snap[0]
	if !rec0.Streamed {
		t.Fatal("stream request should be flagged as streamed")
	}
	if rec0.Usage.TotalTokens != 8 {
		t.Fatalf("total_tokens = %d, want 8", rec0.Usage.TotalTokens)
	}
}
