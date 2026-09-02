package telemetry

import (
	"context"
	"time"
)

// DailyPoint is one aggregate for a model on a day.
type DailyPoint struct {
	Day              string // YYYY-MM-DD
	Model            string
	Requests         int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CostUSD          float64
	AvgLatencyMS     float64
	P95LatencyMS     float64
}

// UsageSummary is the per-day, optional per-model aggregation.
type UsageSummary struct {
	Points []DailyPoint
}

// Range bounds a telemetry query by time and optional model filter.
type Range struct {
	From  time.Time
	To    time.Time
	Model string // empty = all models
}

// Querier reads telemetry history for the dashboard.
type Querier interface {
	UsageSummary(ctx context.Context, r Range) (*UsageSummary, error)
}

// InMemoryQuerier serves summaries from a Buffer, used in dev without
// ClickHouse.
type InMemoryQuerier struct {
	Buffer *Buffer
}

func (q *InMemoryQuerier) UsageSummary(ctx context.Context, r Range) (*UsageSummary, error) {
	if q == nil || q.Buffer == nil {
		return &UsageSummary{Points: []DailyPoint{}}, nil
	}
	byKey := make(map[string]*DailyPoint)
	var order []string
	for _, rec := range q.Buffer.Snapshot() {
		if rec.FinishedAt.Before(r.From) || rec.FinishedAt.After(r.To) {
			continue
		}
		if r.Model != "" && rec.Model != r.Model {
			continue
		}
		key := rec.FinishedAt.Format("2006-01-02") + "|" + rec.Model
		pt, ok := byKey[key]
		if !ok {
			pt = &DailyPoint{Day: rec.FinishedAt.Format("2006-01-02"), Model: rec.Model}
			byKey[key] = pt
			order = append(order, key)
		}
		pt.Requests++
		pt.PromptTokens += rec.Usage.PromptTokens
		pt.CompletionTokens += rec.Usage.CompletionTokens
		pt.TotalTokens += rec.Usage.TotalTokens
		pt.CostUSD += rec.CostUSD
		pt.AvgLatencyMS += float64(rec.Latency.Milliseconds())
	}
	var pts []DailyPoint
	for _, key := range order {
		pt := byKey[key]
		if pt.Requests > 0 {
			pt.AvgLatencyMS /= float64(pt.Requests)
		}
		pts = append(pts, *pt)
	}
	return &UsageSummary{Points: pts}, nil
}
