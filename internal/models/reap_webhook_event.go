package models

import "time"

type ReapWebhookEvent struct {
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	Payload    []byte    `json:"payload"`
	ReceivedAt time.Time `json:"received_at"`
}
