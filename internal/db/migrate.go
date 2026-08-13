package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ponytail: raw idempotent DDL run at startup instead of a migration
// framework; move to golang-migrate if we need versioned/rollback-able
// migrations.
const schema = `
CREATE TABLE IF NOT EXISTS cards (
	card_id TEXT PRIMARY KEY,
	vault_public_key_ecdsa TEXT NOT NULL,
	card_tier TEXT NOT NULL,
	initiate_date TIMESTAMPTZ NOT NULL DEFAULT now(),
	is_active BOOLEAN NOT NULL DEFAULT true,
	nonce BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS cards_vault_public_key_ecdsa_idx ON cards (vault_public_key_ecdsa);
ALTER TABLE cards ADD COLUMN IF NOT EXISTS nonce BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS vault_tokens (
	id TEXT PRIMARY KEY,
	token_id TEXT NOT NULL UNIQUE,
	public_key TEXT NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS vault_tokens_public_key_idx ON vault_tokens (public_key);
`

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, schema)
	return err
}
