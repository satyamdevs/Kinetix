package telemetry

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestPricerCost(t *testing.T) {
	p := DefaultPricer()

	// gpt-4o-mini: $0.15/1M input, $0.60/1M output
	cost := p.Cost("gpt-4o-mini", 1_000_000, 1_000_000)
	if math.Abs(cost-0.75) > 1e-9 {
		t.Fatalf("cost = %v, want 0.75", cost)
	}

	// standalone: 1000 prompt, 500 completion
	cost = p.Cost("gpt-4", 1000, 500)
	want := 30.00*1000/1_000_000 + 60.00*500/1_000_000
	if math.Abs(cost-want) > 1e-9 {
		t.Fatalf("cost = %v, want %v", cost, want)
	}
}

func TestPricerUnknownModel(t *testing.T) {
	p := DefaultPricer()
	if c := p.Cost("gpt-5-unknown", 1000, 1000); c != 0 {
		t.Fatalf("unknown model cost = %v, want 0", c)
	}
}

func TestPricerEmpty(t *testing.T) {
	p := NewPricer(nil)
	if c := p.Cost("gpt-4", 100, 100); c != 0 {
		t.Fatalf("empty pricer cost = %v, want 0", c)
	}
}

func TestBuffer(t *testing.T) {
	b := NewBuffer(2)

	b.Record(context.Background(), Record{RequestID: "1", Model: "a"})
	b.Record(context.Background(), Record{RequestID: "2", Model: "b"})
	if b.Len() != 2 {
		t.Fatalf("len = %d, want 2", b.Len())
	}

	// overflow drops oldest
	b.Record(context.Background(), Record{RequestID: "3", Model: "c"})
	if b.Len() != 2 {
		t.Fatalf("len = %d, want 2 after overflow", b.Len())
	}
	snap := b.Snapshot()
	if snap[0].RequestID != "2" || snap[1].RequestID != "3" {
		t.Fatalf("snapshot order wrong: %+v", snap)
	}
}

func TestDiscard(t *testing.T) {
	d := Discard{}
	if err := d.Record(context.Background(), Record{}); err != nil {
		t.Fatalf("discard should not error: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("discard close: %v", err)
	}
}

func TestRecordFields(t *testing.T) {
	b := NewBuffer(4)
	now := time.Now().UTC()
	rec := Record{
		RequestID:  "req-1",
		APIKeyID:   "key-1",
		Model:      "gpt-4o-mini",
		Provider:   "openai",
		Streamed:   true,
		Status:     200,
		Usage:      Usage{PromptTokens: 1000, CompletionTokens: 1000, TotalTokens: 2000},
		CostUSD:    0.75,
		Latency:    25 * time.Millisecond,
		FinishedAt: now,
	}
	if err := b.Record(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	snap := b.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d", len(snap))
	}
	if snap[0].Usage.TotalTokens != 2000 || snap[0].Latency != 25*time.Millisecond {
		t.Fatalf("record round-trip mismatch: %+v", snap[0])
	}
}
