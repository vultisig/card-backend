package card

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("card not found")

// GetByPublicKey looks up a card by its vault ECDSA public key.
func GetByPublicKey(ctx context.Context, pool *pgxpool.Pool, publicKey string) (*Card, error) {
	var c Card
	err := pool.QueryRow(ctx, `
		SELECT card_id, vault_public_key_ecdsa, card_tier, initiate_date, is_active, nonce
		FROM cards WHERE vault_public_key_ecdsa = $1
	`, publicKey).Scan(&c.CardID, &c.VaultPublicKeyECDSA, &c.CardTier, &c.InitiateDate, &c.IsActive, &c.Nonce)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ClaimNonce atomically advances the card's nonce from expected to expected+1,
// so the same signed nonce can never be replayed. Returns false if the nonce
// didn't match (already used, or wrong value).
func ClaimNonce(ctx context.Context, pool *pgxpool.Pool, cardID string, expected int64) (bool, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE cards SET nonce = nonce + 1 WHERE card_id = $1 AND nonce = $2
	`, cardID, expected)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
