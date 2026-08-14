package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ponytail: raw idempotent DDL run at startup instead of a migration
// framework; move to golang-migrate if we need versioned/rollback-able
// migrations.
const schema = `
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

CREATE TABLE IF NOT EXISTS vultisig_reap_mappings (
	id BIGSERIAL PRIMARY KEY,
	public_key_ecdsa TEXT NOT NULL UNIQUE,
	reap_user_id TEXT UNIQUE,
	nonce BIGINT NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	last_used_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS vultisig_resource_ownership (
	resource_kind TEXT NOT NULL,
	resource_id TEXT NOT NULL,
	reap_user_id TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (resource_kind, resource_id)
);
`

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, schema)
	return err
}
