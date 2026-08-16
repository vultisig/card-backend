// Package cardownership tracks which vault created each REAP card, so
// vault-scoped card actions can reject IDs the calling vault doesn't own.
package cardownership

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

// Record records cardID as owned by publicKey. It's a no-op if cardID is
// already recorded (REAP card IDs are unique, so this only happens on a
// caller retry).
func Record(ctx context.Context, db Querier, publicKey, cardID string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO vultisig_card_ownership (card_id, vault_public_key)
		VALUES ($1, $2)
		ON CONFLICT (card_id) DO NOTHING
	`, cardID, publicKey)
	return err
}

// Owner returns the vault public key recorded as owning cardID. found is
// false, with no error, if cardID has no ownership record at all — callers
// distinguish that from a recorded mismatch, since a missing row can be
// healed from REAP while a mismatch can't.
func Owner(ctx context.Context, db Querier, cardID string) (owner string, found bool, err error) {
	err = db.QueryRow(ctx, `
		SELECT vault_public_key FROM vultisig_card_ownership WHERE card_id = $1
	`, cardID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return owner, true, nil
}
