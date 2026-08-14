package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/reap"
	"github.com/vultisig/card-backend/internal/reapwebhookevent"
)

var ErrInvalidWebhookSignature = reap.ErrInvalidWebhookSignature

// WebhookService backs POST /webhooks/reap, REAP's async event-delivery
// callback (https://docs.reap.global/webhooks/overview). It only verifies
// and persists events for later processing — it does not yet act on any
// event type. The synchronous card-authorization-request webhook is a
// separate REQUEST-mode mechanism REAP calls differently and isn't handled
// here.
type WebhookService struct {
	pool   *pgxpool.Pool
	secret string
}

func NewWebhookService(pool *pgxpool.Pool, secret string) *WebhookService {
	return &WebhookService{pool: pool, secret: secret}
}

// HandleEvent verifies rawBody against signatureHeader (the raw
// X-Reap-Webhook-Signature header value) and, if valid, records the event
// envelope (id/type/rawBody) via reapwebhookevent.Record, which is a no-op
// if the event id was already recorded (REAP redelivers on non-2xx/timeout,
// and may redeliver even after a successful ack). Returns
// ErrInvalidWebhookSignature if verification fails.
func (s *WebhookService) HandleEvent(ctx context.Context, rawBody []byte, signatureHeader string) error {
	if err := reap.VerifyWebhookSignature(s.secret, rawBody, signatureHeader, time.Now()); err != nil {
		return err
	}

	var envelope struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(rawBody, &envelope); err != nil || envelope.ID == "" || envelope.Type == "" {
		return errors.New("reap: webhook payload missing id/type")
	}

	return reapwebhookevent.Record(ctx, s.pool, envelope.ID, envelope.Type, rawBody)
}
