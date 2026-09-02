package keys

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const lookupSQL = `
SELECT id, name, owner, hash, prefix, scopes, enabled,
       requests_per_minute, tokens_per_month, tokens_per_request,
       created_at, expires_at
  FROM api_keys
 WHERE hash = $1`

// Postgres is a Store backed by the api_keys table.
type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Lookup(ctx context.Context, hash string) (*Key, error) {
	var k Key
	var scopes []string
	var exp *time.Time
	err := p.pool.QueryRow(ctx, lookupSQL, hash).Scan(
		&k.ID, &k.Name, &k.Owner, &k.Hash, &k.Prefix, &scopes, &k.Enabled,
		&k.Limit.RequestsPerMinute, &k.Limit.TokensPerMonth, &k.Limit.TokensPerRequest,
		&k.CreatedAt, &exp,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lookup api key: %w", err)
	}
	k.Scopes = scopes
	k.ExpiresAt = exp
	return &k, nil
}

func (p *Postgres) Close() error {
	p.pool.Close()
	return nil
}
