package models

import "time"

type ResourceOwnership struct {
	ResourceKind string    `json:"resource_kind"`
	ResourceID   string    `json:"resource_id"`
	ReapUserID   string    `json:"reap_user_id"`
	CreatedAt    time.Time `json:"created_at"`
}
