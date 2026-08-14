package models

import "time"

type CardOwnership struct {
	CardID         string    `json:"card_id"`
	VaultPublicKey string    `json:"vault_public_key"`
	CreatedAt      time.Time `json:"created_at"`
}
