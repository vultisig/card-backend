// Package resourceownership tracks which REAP user (not vault public key —
// a vault has at most one REAP user, resolved via reapmapping, so this
// tracks ownership the same way REAP itself does) created each REAP
// resource (card, account, ...), so vault-scoped actions can reject IDs the
// calling vault's REAP user doesn't own.
package resourceownership

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Kind identifies the type of REAP resource an ownership row is for, since
// resource IDs are only unique within their own REAP namespace.
type Kind string

const (
	KindCard    Kind = "card"
	KindAccount Kind = "account"
)

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx, so callers can run
// these queries either directly against the pool or inside a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Record records resourceID (of the given kind) as owned by reapUserID.
// It's a no-op if resourceID is already recorded (REAP resource IDs are
// unique per kind, so this only happens on a caller retry).
func Record(ctx context.Context, db Querier, kind Kind, resourceID, reapUserID string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO vultisig_resource_ownership (resource_kind, resource_id, reap_user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (resource_kind, resource_id) DO NOTHING
	`, kind, resourceID, reapUserID)
	return err
}

// IsOwner reports whether resourceID (of the given kind) is recorded as
// owned by reapUserID. It returns false, not an error, if resourceID has no
// ownership record at all.
func IsOwner(ctx context.Context, db Querier, kind Kind, resourceID, reapUserID string) (bool, error) {
	var owner string
	err := db.QueryRow(ctx, `
		SELECT reap_user_id FROM vultisig_resource_ownership WHERE resource_kind = $1 AND resource_id = $2
	`, kind, resourceID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return owner == reapUserID, nil
}
