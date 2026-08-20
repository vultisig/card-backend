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

// IsOwner reports whether cardID is recorded as owned by publicKey. It
// returns false, not an error, if cardID has no ownership record at all.
func IsOwner(ctx context.Context, db Querier, publicKey, cardID string) (bool, error) {
	var owner string
	err := db.QueryRow(ctx, `
		SELECT vault_public_key FROM vultisig_card_ownership WHERE card_id = $1
	`, cardID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return owner == publicKey, nil
}

// OwnerOf returns the vault public key that owns cardID, or "" if cardID
// has no ownership record at all.
func OwnerOf(ctx context.Context, db Querier, cardID string) (string, error) {
	var owner string
	err := db.QueryRow(ctx, `
		SELECT vault_public_key FROM vultisig_card_ownership WHERE card_id = $1
	`, cardID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return owner, err
}
