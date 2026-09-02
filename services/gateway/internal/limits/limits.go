package limits

import (
	"context"
	"errors"
	"time"
)

var ErrStore = errors.New("limit store unavailable")

// Decision reports whether a limit allows the operation and how much headroom
// remains (negative remaining means the cap was exceeded).
type Decision struct {
	OK        bool
	Remaining int64
}

// Policy carries the configured limits applied to a key. Zero values disable
// their respective limit.
type Policy struct {
	RequestsPerMinute int64
	TokensPerMonth    int64
	Window            time.Duration
}

// Limiter tracks fixed-window request counts and cumulative token spend in a
// shared store (Redis in production, Memory in tests/dev).
type Limiter interface {
	// Allow counts one request for id in the current fixed window and reports
	// whether it stays under limit. A limit <= 0 is treated as unlimited.
	Allow(ctx context.Context, id string, limit int64, window time.Duration) (Decision, error)
	// Tokens reports the cumulative token spend recorded for id.
	Tokens(ctx context.Context, id string) (int64, error)
	// AddTokens reserves n tokens for id. When cap > 0 the decision reports
	// whether the cumulative total now exceeds it; the spend is still recorded.
	AddTokens(ctx context.Context, id string, n int64, cap int64) (Decision, error)
	// ResetTokens zeroes the cumulative token spend for id (monthly rollover).
	ResetTokens(ctx context.Context, id string) error
}

// DefaultWindow is the fixed rate-limit window.
const DefaultWindow = time.Minute
