// Package vaulttoken persists issued JWT access tokens (vault_tokens), so a
// presented token can be checked against a durable, revocable record
// instead of trusting the JWT signature alone.
package vaulttoken

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vultisig/card-backend/internal/models"
)

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx, so callers can run
// these queries either directly against the pool or inside a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Record persists a newly issued access token identified by tokenID (the
// JWT's jti claim), owned by publicKey and expiring at expiresAt.
func Record(ctx context.Context, db Querier, tokenID, publicKey string, expiresAt time.Time) error {
	_, err := db.Exec(ctx, `
		INSERT INTO vault_tokens (id, token_id, public_key, expires_at)
		VALUES ($1, $1, $2, $3)
	`, tokenID, publicKey, expiresAt)
	return err
}

// Touch looks up tokenID (a JWT's jti claim), bumping its last_used_at, and
// returns the resulting row — or nil, not an error, if tokenID has no
// record at all (never issued by this server, or the DB was reset).
// Callers still need to check the returned token's IsRevoked() and
// ExpiresAt themselves.
func Touch(ctx context.Context, db Querier, tokenID string) (*models.VaultToken, error) {
	tok := &models.VaultToken{}
	err := db.QueryRow(ctx, `
		UPDATE vault_tokens SET last_used_at = now()
		WHERE token_id = $1
		RETURNING id, token_id, public_key, expires_at, created_at, updated_at, last_used_at, revoked_at
	`, tokenID).Scan(&tok.ID, &tok.TokenID, &tok.PublicKey, &tok.ExpiresAt, &tok.CreatedAt, &tok.UpdatedAt, &tok.LastUsedAt, &tok.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return tok, nil
}
