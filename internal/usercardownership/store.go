// Package usercardownership tracks which REAP user each REAP card belongs
// to, mirroring REAP's own ownership model (a card's userId) rather than
// our vault-facing shadow copy in cardownership. Kept as its own table,
// separate from cardownership, so the vault-keyed and REAP-user-keyed
// relationships can each be indexed/optimized independently as they grow.
package usercardownership

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

// Record records cardID as owned by reapUserID. It's a no-op if cardID is
// already recorded (REAP card IDs are unique, so this only happens on a
// caller retry).
func Record(ctx context.Context, db Querier, reapUserID, cardID string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO vultisig_user_card_ownership (card_id, reap_user_id)
		VALUES ($1, $2)
		ON CONFLICT (card_id) DO NOTHING
	`, cardID, reapUserID)
	return err
}

// IsOwner reports whether cardID is recorded as owned by reapUserID. It
// returns false, not an error, if cardID has no ownership record at all.
func IsOwner(ctx context.Context, db Querier, reapUserID, cardID string) (bool, error) {
	var owner string
	err := db.QueryRow(ctx, `
		SELECT reap_user_id FROM vultisig_user_card_ownership WHERE card_id = $1
	`, cardID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return owner == reapUserID, nil
}
