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
