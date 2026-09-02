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

const createSQL = `
INSERT INTO api_keys
       (id, name, owner, hash, prefix, scopes, enabled,
        requests_per_minute, tokens_per_month, tokens_per_request,
        created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (id) DO NOTHING`

func (p *Postgres) Create(ctx context.Context, k *Key) error {
	tag, err := p.pool.Exec(ctx, createSQL,
		k.ID, k.Name, k.Owner, k.Hash, k.Prefix, k.Scopes, k.Enabled,
		k.Limit.RequestsPerMinute, k.Limit.TokensPerMonth, k.Limit.TokensPerRequest,
		k.CreatedAt, k.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const enableSQL = `UPDATE api_keys SET enabled = $2 WHERE id = $1`

func (p *Postgres) SetEnabled(ctx context.Context, id string, enabled bool) error {
	tag, err := p.pool.Exec(ctx, enableSQL, id, enabled)
	if err != nil {
		return fmt.Errorf("update api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const deleteSQL = `DELETE FROM api_keys WHERE id = $1`

func (p *Postgres) Delete(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, deleteSQL, id)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const listSQL = `
SELECT id, name, owner, hash, prefix, scopes, enabled,
       requests_per_minute, tokens_per_month, tokens_per_request,
       created_at, expires_at
  FROM api_keys
 ORDER BY created_at DESC`

func (p *Postgres) List(ctx context.Context) ([]*Key, error) {
	rows, err := p.pool.Query(ctx, listSQL)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var out []*Key
	for rows.Next() {
		var k Key
		var scopes []string
		var exp *time.Time
		if err := rows.Scan(
			&k.ID, &k.Name, &k.Owner, &k.Hash, &k.Prefix, &scopes, &k.Enabled,
			&k.Limit.RequestsPerMinute, &k.Limit.TokensPerMonth, &k.Limit.TokensPerRequest,
			&k.CreatedAt, &exp,
		); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		k.Scopes = scopes
		k.ExpiresAt = exp
		out = append(out, &k)
	}
	return out, rows.Err()
}

func (p *Postgres) Close() error {
	p.pool.Close()
	return nil
}
