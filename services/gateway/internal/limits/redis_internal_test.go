package limits

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisWindowRollover(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	r := NewRedis(rdb)
	clock := time.Unix(1_700_000_000, 0)
	r.now = func() time.Time { return clock }

	d, err := r.Allow(context.Background(), "k", 1, time.Minute)
	if err != nil || !d.OK {
		t.Fatalf("first request: %+v err=%v", d, err)
	}
	d, err = r.Allow(context.Background(), "k", 1, time.Minute)
	if err != nil || d.OK {
		t.Fatalf("second request in same window should block: %+v err=%v", d, err)
	}

	clock = clock.Add(time.Minute)
	d, err = r.Allow(context.Background(), "k", 1, time.Minute)
	if err != nil || !d.OK {
		t.Fatalf("new window should allow: %+v err=%v", d, err)
	}
}
