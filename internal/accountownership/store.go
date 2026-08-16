// Package accountownership tracks which vault created each REAP account, so
// vault-scoped account filters (e.g. the /activities accountId filter) can
// reject IDs the calling vault doesn't own. Same table/query shape as
// cardownership, kept as a separate table so each resource type can be
// indexed/optimized independently as it grows.
package accountownership

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx, so callers can run
// these queries either directly against the pool or inside a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Record records accountID as owned by publicKey. It's a no-op if accountID
// is already recorded (REAP account IDs are unique, so this only happens on
// a caller retry).
func Record(ctx context.Context, db Querier, publicKey, accountID string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO vultisig_account_ownership (account_id, vault_public_key)
		VALUES ($1, $2)
		ON CONFLICT (account_id) DO NOTHING
	`, accountID, publicKey)
	return err
}

// Owner returns the vault public key recorded as owning accountID. found is
// false, with no error, if accountID has no ownership record at all —
// callers distinguish that from a recorded mismatch, since a missing row
// can be healed from REAP while a mismatch can't.
func Owner(ctx context.Context, db Querier, accountID string) (owner string, found bool, err error) {
	err = db.QueryRow(ctx, `
		SELECT vault_public_key FROM vultisig_account_ownership WHERE account_id = $1
	`, accountID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return owner, true, nil
}
