package card

import "time"

type Card struct {
	CardID              string    `json:"card_id"`
	VaultPublicKeyECDSA string    `json:"vault_public_key_ecdsa"`
	CardTier            string    `json:"card_tier"`
	InitiateDate        time.Time `json:"initiate_date"`
	IsActive            bool      `json:"is_active"`
}
