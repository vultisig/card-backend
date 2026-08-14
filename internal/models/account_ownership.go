package models

import "time"

type AccountOwnership struct {
	AccountID      string    `json:"account_id"`
	VaultPublicKey string    `json:"vault_public_key"`
	CreatedAt      time.Time `json:"created_at"`
}
