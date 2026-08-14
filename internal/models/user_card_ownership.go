package models

import "time"

type UserCardOwnership struct {
	CardID     string    `json:"card_id"`
	ReapUserID string    `json:"reap_user_id"`
	CreatedAt  time.Time `json:"created_at"`
}
