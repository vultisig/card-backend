package models

import "time"

type VultisigReapMapping struct {
	ID             int64     `json:"id"`
	PublicKeyECDSA string    `json:"public_key_ecdsa"`
	ReapUserID     string    `json:"reap_user_id"`
	Nonce          int64     `json:"nonce"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastUsedAt     time.Time `json:"last_used_at"`
}
