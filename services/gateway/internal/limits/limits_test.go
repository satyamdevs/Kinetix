package limits_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"orca/gateway/internal/limits"
)

func TestMemoryAllowFixedWindow(t *testing.T) {
	l := limits.NewMemory()

	for i := 0; i < 2; i++ {
		d, err := l.Allow(context.Background(), "k", 2, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if !d.OK {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	d, err := l.Allow(context.Background(), "k", 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if d.OK {
		t.Fatal("third request should be rejected")
	}

	d, _ = l.Allow(context.Background(), "other", 2, time.Minute)
	if !d.OK {
		t.Fatal("other key must not share the counter")
	}
}

func TestMemoryUnlimited(t *testing.T) {
	l := limits.NewMemory()
	for i := 0; i < 100; i++ {
		d, err := l.Allow(context.Background(), "k", 0, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if !d.OK {
			t.Fatal("limit 0 should be unlimited")
		}
	}
}

func TestMemoryTokenQuota(t *testing.T) {
	l := limits.NewMemory()

	d, err := l.AddTokens(context.Background(), "k", 400, 500)
	if err != nil || !d.OK {
		t.Fatalf("under cap should pass: %+v %v", d, err)
	}
	d, _ = l.AddTokens(context.Background(), "k", 200, 500)
	if d.OK {
		t.Fatalf("cumulative 600 > 500 should fail, got %+v", d)
	}
	if d.Remaining != -100 {
		t.Fatalf("remaining = %d, want -100", d.Remaining)
	}
	total, err := l.Tokens(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if total != 600 {
		t.Fatalf("total = %d, want 600", total)
	}

	if err := l.ResetTokens(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}
	after, err := l.Tokens(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Fatalf("after reset total = %d, want 0", after)
	}
}

func TestRedisBackend(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	l := limits.NewRedis(rdb)

	d, err := l.Allow(context.Background(), "k", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !d.OK {
		t.Fatal("first request should pass")
	}
	d, err = l.Allow(context.Background(), "k", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if d.OK {
		t.Fatal("second request should be rate limited")
	}

	if _, err := l.AddTokens(context.Background(), "k", 50, 100); err != nil {
		t.Fatal(err)
	}
	spent, err := l.Tokens(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if spent != 50 {
		t.Fatalf("spent = %d, want 50", spent)
	}
}
