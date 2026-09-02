package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
)

const insertSQL = `
INSERT INTO chat_completions
    (ts, day, request_id, api_key_id, model, provider, streamed, status,
     prompt_tokens, completion_tokens, total_tokens, cost_usd, latency_ms, error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?,
        ?, ?, ?, ?, ?, ?)`

// ClickHouse is a Sink that batches completions into ClickHouse.
type ClickHouse struct {
	db *sql.DB
}

// Open connects to ClickHouse and pings the telemetry database. The table is
// expected to be created by the deploy migration.
func Open(ctx context.Context, dsn string) (*ClickHouse, error) {
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}
	return &ClickHouse{db: db}, nil
}

func (c *ClickHouse) Record(ctx context.Context, r Record) error {
	day := r.FinishedAt.Format("2006-01-02")

	// Best-effort: batches give better throughput, but a single-row insert is
	// retained for simplicity and low latency. Isolation choice avoids write
	// amplification in a single-process gateway.
	_, err := c.db.ExecContext(ctx, insertSQL,
		r.FinishedAt.Format("2006-01-02 15:04:05"),
		day,
		or(r.RequestID, "unknown"),
		or(r.APIKeyID, "unknown"),
		r.Model,
		r.Provider,
		boolToUint8(r.Streamed),
		r.Status,
		r.Usage.PromptTokens,
		r.Usage.CompletionTokens,
		r.Usage.TotalTokens,
		r.CostUSD,
		millis(r.Latency),
		or(r.Error, ""),
	)
	if err != nil {
		slog.Error("telemetry write failed", "err", err)
		return err
	}
	return nil
}

func (c *ClickHouse) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func boolToUint8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

func millis(d time.Duration) int {
	return int(d.Milliseconds())
}
