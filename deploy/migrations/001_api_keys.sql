-- Phase 3: governance schema (PostgreSQL)
-- Applied by the gateway at startup via POSTGRES_DSN.

CREATE TABLE IF NOT EXISTS api_keys (
    id                   VARCHAR(64)  PRIMARY KEY,
    name                 TEXT         NOT NULL,
    owner                TEXT         NOT NULL DEFAULT '',
    hash                 CHAR(64)     NOT NULL UNIQUE,     -- SHA-256 of the raw key
    prefix               TEXT         NOT NULL,            -- display-only prefix
    scopes               TEXT[]       NOT NULL DEFAULT '{}',
    enabled              BOOLEAN      NOT NULL DEFAULT TRUE,
    requests_per_minute  BIGINT       NOT NULL DEFAULT 500, -- 0 = unlimited
    tokens_per_month     BIGINT       NOT NULL DEFAULT 0,   -- 0 = unlimited
    tokens_per_request   BIGINT       NOT NULL DEFAULT 0,   -- 0 = unlimited
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    expires_at           TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys (hash);