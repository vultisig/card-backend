package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/accountownership"
	"github.com/vultisig/card-backend/internal/cardownership"
	"github.com/vultisig/card-backend/internal/notification"
	"github.com/vultisig/card-backend/internal/reap"
	"github.com/vultisig/card-backend/internal/reapmapping"
	"github.com/vultisig/card-backend/internal/reapwebhookevent"
)

var (
	ErrInvalidWebhookSignature = reap.ErrInvalidWebhookSignature
	// ErrInvalidWebhookPayload is returned when a correctly signed webhook
	// body doesn't parse as a valid event envelope. Retrying can't fix a
	// malformed envelope, so callers should map this to a 4xx (not 5xx) to
	// stop REAP's automatic retries.
	ErrInvalidWebhookPayload = errors.New("reap: webhook payload missing id/type")
)

// WebhookService backs POST /webhooks/reap, REAP's async event-delivery
// callback (https://docs.reap.global/webhooks/overview). It verifies and
// persists every event, then best-effort pushes a client notification for
// the vault the event concerns. The synchronous card-authorization-request
// webhook is a separate REQUEST-mode mechanism REAP calls differently and
// isn't handled here.
type WebhookService struct {
	pool         *pgxpool.Pool
	secret       string
	notification *notification.Client
}

func NewWebhookService(pool *pgxpool.Pool, secret string, notificationClient *notification.Client) *WebhookService {
	return &WebhookService{pool: pool, secret: secret, notification: notificationClient}
}

// HandleEvent verifies rawBody against signatureHeader (the raw
// X-Reap-Webhook-Signature header value) and, if valid, records the event
// envelope (id/type/rawBody) via reapwebhookevent.Record, which is a no-op
// if the event id was already recorded (REAP redelivers on non-2xx/timeout,
// and may redeliver even after a successful ack). The client notification
// only fires on the first-time recording of an event id, so a redelivery
// never double-notifies. Returns ErrInvalidWebhookSignature if verification
// fails.
func (s *WebhookService) HandleEvent(ctx context.Context, rawBody []byte, signatureHeader string) error {
	if err := reap.VerifyWebhookSignature(s.secret, rawBody, signatureHeader, time.Now()); err != nil {
		return err
	}

	var envelope struct {
		ID   string          `json:"id"`
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rawBody, &envelope); err != nil || envelope.ID == "" || envelope.Type == "" {
		return ErrInvalidWebhookPayload
	}

	inserted, err := reapwebhookevent.Record(ctx, s.pool, envelope.ID, envelope.Type, rawBody)
	if err != nil {
		return err
	}
	if !inserted {
		// Redelivery of an event we've already processed — REAP retries on
		// non-2xx/timeout and may redeliver even after a successful ack, so
		// notifying again here would double-push the same event.
		return nil
	}

	s.notifyVault(ctx, envelope.Type, envelope.Data)
	return nil
}

// notifyVault best-effort pushes a client notification for the vault
// affected by a REAP event. Resolution misses (unrecognized event shape, no
// ownership record, no recorded chain code) and notification-service errors
// are logged, not returned: the event is already durably recorded by the
// time this runs, so a downstream notification hiccup must not fail
// HandleEvent and trigger a REAP retry of an event we've already stored.
func (s *WebhookService) notifyVault(ctx context.Context, eventType string, data json.RawMessage) {
	publicKey, err := s.resolveVault(ctx, eventType, data)
	if err != nil {
		log.Printf("webhook: resolve vault for %s event: %v", eventType, err)
		return
	}
	if publicKey == "" {
		return
	}

	hexChainCode, err := reapmapping.GetHexChainCode(ctx, s.pool, publicKey)
	if err != nil {
		log.Printf("webhook: get chain code: %v", err)
		return
	}
	if hexChainCode == "" {
		return
	}

	err = s.notification.Notify(ctx, notification.Request{
		VaultID: notification.VaultID(publicKey, hexChainCode),
		Type:    "generic",
		Title:   "Vultisig Card",
		Body:    describeEvent(eventType),
	})
	if err != nil {
		log.Printf("webhook: notify: %v", err)
	}
}

// resolveVault maps a REAP webhook event to the vault it concerns, using
// the same ownership tables the REST handlers check requests against.
// Returns "" (no error) if the event type/shape isn't resolvable to a vault
// yet, or if the resolved ID has no ownership record.
func (s *WebhookService) resolveVault(ctx context.Context, eventType string, data json.RawMessage) (string, error) {
	cardID, accountID, userID := eventSubjectIDs(eventType, data)
	switch {
	case cardID != "":
		return cardownership.OwnerOf(ctx, s.pool, cardID)
	case accountID != "":
		return accountownership.OwnerOf(ctx, s.pool, accountID)
	case userID != "":
		return reapmapping.GetPublicKeyByReapUserID(ctx, s.pool, userID)
	default:
		return "", nil
	}
}

// eventSubjectIDs extracts the id REAP's webhook data identifies its
// subject by. Per REAP's docs, a webhook's data "match[es] the shape of the
// corresponding API resource" (GET /resource/:id), so the field to key off
// of depends on the event type:
//   - card-scoped resources carry a cardId: CARD_TRANSACTION_CREATED/
//     UPDATED (transaction), CARD_FRAUD_ALERT_CREATED/STATUS_UPDATED (fraud
//     alert), CARD_3DS_CHALLENGE_CREATED (3DS challenge), and
//     CARD_DISPUTE_STATUS_UPDATED (dispute) — same shape CardTransaction/
//     FraudAlert/CardService already read (internal/service/cardtransaction.go,
//     fraudalert.go, card.go)
//   - CARD_SHIPMENT_STATUS_UPDATED's data is the shipment resource, carrying
//     cards[].cardId (a shipment can hold more than one card; the first is
//     used — see internal/service/cardshipment.go)
//   - CARD_STATUS_UPDATED's data IS the card resource, keyed by its own id
//   - account-scoped resources carry an accountId: CRYPTO_DEPOSIT_CREATED/
//     STATUS_UPDATED (deposit)
//   - ACCOUNT_STATUS_UPDATED's data is the account resource, keyed by id
//   - USER_APPLICATION_STATUS_UPDATED's data is the user resource, keyed by
//     id (REAP's user id, reverse-mapped to a vault via reapmapping)
//
// Returns exactly one of cardID/accountID/userID set, or all empty if
// eventType/data don't resolve to any of the above.
//
// ponytail: COMPANY_STATUS_UPDATED isn't resolved — no local ownership
// table for a company. CARD_TOKENIZATION_REQUESTED isn't resolved either:
// REAP's docs don't publish a payload schema for it, and the resource it
// most plausibly corresponds to (the push-provisioning response) has no
// cardId field at all (the card is identified by the request path, not the
// response body) — add a case for it once a real delivered payload is seen.
func eventSubjectIDs(eventType string, data json.RawMessage) (cardID, accountID, userID string) {
	var payload struct {
		ID        string `json:"id"`
		CardID    string `json:"cardId"`
		AccountID string `json:"accountId"`
		Cards     []struct {
			CardID string `json:"cardId"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", "", ""
	}

	cardID = payload.CardID
	if cardID == "" && len(payload.Cards) > 0 {
		cardID = payload.Cards[0].CardID
	}
	if cardID == "" && eventType == "CARD_STATUS_UPDATED" {
		cardID = payload.ID
	}
	if cardID != "" {
		return cardID, "", ""
	}

	accountID = payload.AccountID
	if accountID == "" && eventType == "ACCOUNT_STATUS_UPDATED" {
		accountID = payload.ID
	}
	if accountID != "" {
		return "", accountID, ""
	}

	if eventType == "USER_APPLICATION_STATUS_UPDATED" && payload.ID != "" {
		return "", "", payload.ID
	}
	return "", "", ""
}

// describeEvent turns a REAP event type constant (e.g. "CARD_STATUS_UPDATED")
// into a human-readable notification body ("card status updated").
func describeEvent(eventType string) string {
	return strings.ToLower(strings.ReplaceAll(eventType, "_", " "))
}
