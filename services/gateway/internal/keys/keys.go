package keys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const KeyPrefix = "sk-orca-"

// Limit is the per-key quota. A zero value means no cap for that metric.
type Limit struct {
	RequestsPerMinute int64 `json:"requests_per_minute"` // 0 = unlimited
	TokensPerMonth    int64 `json:"tokens_per_month"`    // 0 = unlimited
	TokensPerRequest  int64 `json:"tokens_per_request"`  // 0 = unlimited
}

// Key is the governance record for a single API key. The raw secret is shown
// only at provisioning time; stores persist Hash.
type Key struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Owner     string     `json:"owner"`
	Hash      string     `json:"-"`
	Prefix    string     `json:"prefix"`
	Scopes    []string   `json:"scopes"`
	Enabled   bool       `json:"enabled"`
	Limit     Limit      `json:"limit"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

var (
	ErrNotFound = errors.New("api key not found")
	ErrDisabled = errors.New("api key disabled")
	ErrExpired  = errors.New("api key expired")
)

// Store looks up the governance record for a hashed API key.
type Store interface {
	Lookup(ctx context.Context, hash string) (*Key, error)
	Close() error
}

// Hash returns the stable identifier persisted for a raw API key.
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Validate reports whether a key is usable right now.
func Validate(k *Key) error {
	if k == nil {
		return ErrNotFound
	}
	if !k.Enabled {
		return ErrDisabled
	}
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		return ErrExpired
	}
	return nil
}

// Generate creates a new random API key and the record to be stored. The raw
// secret is returned exactly once; callers persist only key.Hash.
func Generate(name, owner string, limit Limit, ttl time.Duration) (raw string, k *Key, err error) {
	buf := make([]byte, 24)
	if _, err = rand.Read(buf); err != nil {
		return "", nil, err
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	raw = KeyPrefix + strings.ToLower(secret)

	now := time.Now().UTC()
	var exp *time.Time
	if ttl > 0 {
		e := now.Add(ttl)
		exp = &e
	}
	k = &Key{
		ID:        randomID(),
		Name:      name,
		Owner:     owner,
		Hash:      Hash(raw),
		Prefix:    raw[:len(KeyPrefix)+5],
		Enabled:   true,
		Limit:     limit,
		CreatedAt: now,
		ExpiresAt: exp,
	}
	return raw, k, nil
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
