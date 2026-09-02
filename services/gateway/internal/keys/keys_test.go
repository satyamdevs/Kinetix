package keys

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHash(t *testing.T) {
	a := Hash("sk-orca-test")
	if len(a) != 64 {
		t.Fatalf("hash length = %d, want 64", len(a))
	}
	if Hash("sk-orca-test") != a {
		t.Fatal("hash must be deterministic")
	}
}

func TestMemoryLookup(t *testing.T) {
	raw, k, err := Generate("ci", "tester", Limit{RequestsPerMinute: 10}, 0)
	if err != nil {
		t.Fatal(err)
	}
	m := NewMemory()
	m.Upsert(k)

	got, err := m.Lookup(context.Background(), Hash(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != k.ID || got.Hash != k.Hash {
		t.Fatalf("lookup mismatch: %+v vs %+v", got, k)
	}

	if _, err := m.Lookup(context.Background(), Hash("sk-orca-missing")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestGenerateKeyShape(t *testing.T) {
	raw, k, err := Generate("dev", "local", Limit{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, KeyPrefix) {
		t.Fatalf("raw key %q missing prefix", raw)
	}
	if k.Hash == "" || k.Hash != Hash(raw) {
		t.Fatalf("hash not derived from raw key")
	}
	if k.Prefix == "" || strings.Contains(k.Prefix, raw[10:]) {
		t.Fatalf("prefix must not leak the full key: %q", k.Prefix)
	}
	if k.ExpiresAt == nil || !k.ExpiresAt.After(time.Now()) {
		t.Fatal("ttl key should carry a future expiry")
	}
}

func TestValidate(t *testing.T) {
	_, k, _ := Generate("a", "b", Limit{}, 0)
	if err := Validate(k); err != nil {
		t.Fatalf("enabled key rejected: %v", err)
	}
	if err := Validate(nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nil key: want ErrNotFound, got %v", err)
	}

	k.Enabled = false
	if err := Validate(k); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled key: want ErrDisabled, got %v", err)
	}

	_, expired, _ := Generate("a", "b", Limit{}, 0)
	past := time.Now().Add(-time.Hour)
	expired.ExpiresAt = &past
	if err := Validate(expired); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired key: want ErrExpired, got %v", err)
	}
}

func TestMemoryKeeperLifecycle(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()

	raw1, k1, _ := Generate("ci", "t", Limit{RequestsPerMinute: 5}, 0)
	raw2, k2, _ := Generate("ci", "t", Limit{TokensPerMonth: 100}, time.Hour)

	if err := m.Create(ctx, k1); err != nil {
		t.Fatalf("create k1: %v", err)
	}
	if err := m.Create(ctx, k2); err != nil {
		t.Fatalf("create k2: %v", err)
	}
	if err := m.Create(ctx, k1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("duplicate create: want ErrNotFound, got %v", err)
	}

	all, err := m.List(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("list = %d, err = %v", len(all), err)
	}

	if err := m.SetEnabled(ctx, k1.ID, false); err != nil {
		t.Fatal(err)
	}
	got, err := m.Lookup(ctx, Hash(raw1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatal("disabled key should report disabled")
	}
	if _, err := m.Lookup(ctx, Hash(raw2)); err != nil {
		t.Fatalf("enabled key lookup: %v", err)
	}

	if err := m.Delete(ctx, k2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Lookup(ctx, Hash(raw2)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked key: want ErrNotFound, got %v", err)
	}
	if err := m.SetEnabled(ctx, "missing", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: want ErrNotFound, got %v", err)
	}
}
