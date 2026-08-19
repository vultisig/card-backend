package reapmapping

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx, so callers can run
// these queries either directly against the pool or inside a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// GetNonce returns the next expected nonce for publicKey. A vault with no
// mapping row yet has an implicit nonce of 0.
func GetNonce(ctx context.Context, db Querier, publicKey string) (int64, error) {
	var nonce int64
	err := db.QueryRow(ctx, `
		SELECT nonce FROM vultisig_reap_mappings WHERE public_key_ecdsa = $1
	`, publicKey).Scan(&nonce)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return nonce, err
}

// GetReapUserID returns publicKey's REAP user ID, or "" if no mapping row
// exists yet or none has been set.
func GetReapUserID(ctx context.Context, db Querier, publicKey string) (string, error) {
	var reapUserID *string
	err := db.QueryRow(ctx, `
		SELECT reap_user_id FROM vultisig_reap_mappings WHERE public_key_ecdsa = $1
	`, publicKey).Scan(&reapUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if reapUserID == nil {
		return "", nil
	}
	return *reapUserID, nil
}

// SetReapUserID sets publicKey's REAP user ID, creating the mapping row on
// first use, and clears any in-flight create claim (see
// ClaimReapUserCreate). It only sets the value if one isn't already set,
// returning false if publicKey already had a REAP user ID.
func SetReapUserID(ctx context.Context, db Querier, publicKey, reapUserID string) (bool, error) {
	tag, err := db.Exec(ctx, `
		INSERT INTO vultisig_reap_mappings (public_key_ecdsa, reap_user_id, updated_at, last_used_at)
		VALUES ($1, $2, now(), now())
		ON CONFLICT (public_key_ecdsa) DO UPDATE
			SET reap_user_id = $2, reap_user_create_started_at = NULL, updated_at = now(), last_used_at = now()
			WHERE vultisig_reap_mappings.reap_user_id IS NULL
	`, publicKey, reapUserID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ClaimNonce atomically advances publicKey's nonce from expected to
// expected+1, creating the mapping row on first use, so the same signed
// nonce can never be replayed. Returns false if the nonce didn't match
// (already used, or wrong value).
func ClaimNonce(ctx context.Context, db Querier, publicKey string, expected int64) (bool, error) {
	tag, err := db.Exec(ctx, `
		INSERT INTO vultisig_reap_mappings (public_key_ecdsa, nonce, updated_at, last_used_at)
		VALUES ($1, $2 + 1, now(), now())
		ON CONFLICT (public_key_ecdsa) DO UPDATE
			SET nonce = vultisig_reap_mappings.nonce + 1, updated_at = now(), last_used_at = now()
			WHERE vultisig_reap_mappings.nonce = $2
	`, publicKey, expected)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ClaimReapUserCreate marks publicKey as having a REAP user creation in flight, creating the row
// on first use. False if it already has a user ID or a claim under staleAfter old; older ones are
// treated as abandoned and taken over.
func ClaimReapUserCreate(ctx context.Context, db Querier, publicKey string, staleAfter time.Duration) (bool, error) {
	tag, err := db.Exec(ctx, `
		INSERT INTO vultisig_reap_mappings (public_key_ecdsa, reap_user_create_started_at, updated_at, last_used_at)
		VALUES ($1, now(), now(), now())
		ON CONFLICT (public_key_ecdsa) DO UPDATE
			SET reap_user_create_started_at = now(), updated_at = now(), last_used_at = now()
			WHERE vultisig_reap_mappings.reap_user_id IS NULL
				AND (vultisig_reap_mappings.reap_user_create_started_at IS NULL
					OR vultisig_reap_mappings.reap_user_create_started_at < now() - make_interval(secs => $2))
	`, publicKey, staleAfter.Seconds())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ReleaseReapUserCreate hands the claim back so a failed attempt can retry without waiting it out.
func ReleaseReapUserCreate(ctx context.Context, db Querier, publicKey string) error {
	_, err := db.Exec(ctx, `
		UPDATE vultisig_reap_mappings
		SET reap_user_create_started_at = NULL, updated_at = now()
		WHERE public_key_ecdsa = $1 AND reap_user_id IS NULL
	`, publicKey)
	return err
}
