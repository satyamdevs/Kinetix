package limits

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	rateKey   = "orca:rate:%s:%d:%d" // id:window-bucket
	tokensKey = "orca:tokens:%s"     // id cumulative spend
	keepTTL   = 2 * time.Minute
)

// Redis implements Limiter on top of a shared Redis instance. Fixed-window
// counters expire after the window; cumulative token spend has no expiry and
// must be reset when a key's monthly period rolls over.
type Redis struct {
	rdb redis.UniversalClient
	now func() time.Time
}

func NewRedis(rdb redis.UniversalClient) *Redis {
	return &Redis{rdb: rdb, now: time.Now}
}

func (r *Redis) Allow(ctx context.Context, id string, limit int64, window time.Duration) (Decision, error) {
	if limit <= 0 {
		return Decision{OK: true}, nil
	}
	if window <= 0 {
		window = DefaultWindow
	}
	bucket := r.now().Unix() / int64(window.Seconds())
	rkey := fmt.Sprintf(rateKey, id, int64(window.Seconds()), bucket)

	pipe := r.rdb.TxPipeline()
	incr := pipe.Incr(ctx, rkey)
	pipe.Expire(ctx, rkey, window+keepTTL)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return Decision{}, fmt.Errorf("%w: %v", ErrStore, err)
	}

	count := incr.Val()
	return Decision{OK: count <= limit, Remaining: limit - count}, nil
}

func (r *Redis) Tokens(ctx context.Context, id string) (int64, error) {
	n, err := r.rdb.Get(ctx, fmt.Sprintf(tokensKey, id)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrStore, err)
	}
	return n, nil
}

func (r *Redis) AddTokens(ctx context.Context, id string, n, cap int64) (Decision, error) {
	key := fmt.Sprintf(tokensKey, id)
	total, err := r.rdb.IncrBy(ctx, key, n).Result()
	if err != nil {
		return Decision{}, fmt.Errorf("%w: %v", ErrStore, err)
	}
	if cap > 0 && total > cap {
		return Decision{OK: false, Remaining: cap - total}, nil
	}
	return Decision{OK: true, Remaining: total}, nil
}

func (r *Redis) ResetTokens(ctx context.Context, id string) error {
	if err := r.rdb.Del(ctx, fmt.Sprintf(tokensKey, id)).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrStore, err)
	}
	return nil
}
