package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// No-ops if s.notification is nil (NOTIFICATION_REDIS_URL unset/invalid at
// startup — webhook notifications are optional).
func (s *WebhookService) notifyVault(ctx context.Context, eventType string, data json.RawMessage) {
	if s.notification == nil {
		return
	}

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
		Body:    notificationBody(eventType, data),
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

// notificationBody builds a human-readable notification body for a REAP
// event, pulling the relevant fields out of data (the resource shape noted
// on eventSubjectIDs). Falls back to a humanized event type for any event
// not covered below (currently unreachable in practice — resolveVault only
// resolves a vault, and so only calls this, for the event types handled
// here — but kept so a future case added to eventSubjectIDs without a
// matching one here still produces a readable body instead of an empty one).
func notificationBody(eventType string, data json.RawMessage) string {
	switch eventType {
	case "USER_APPLICATION_STATUS_UPDATED":
		return userApplicationBody(data)
	case "CRYPTO_DEPOSIT_CREATED":
		return depositBody(data, "detected")
	case "CRYPTO_DEPOSIT_STATUS_UPDATED":
		return depositBody(data, "updated")
	case "CARD_TRANSACTION_CREATED":
		return transactionBody(data, "New")
	case "CARD_TRANSACTION_UPDATED":
		return transactionBody(data, "Updated")
	case "CARD_FRAUD_ALERT_CREATED", "CARD_FRAUD_ALERT_STATUS_UPDATED":
		return fraudAlertBody(data)
	case "CARD_SHIPMENT_STATUS_UPDATED":
		return shipmentBody(data)
	case "CARD_DISPUTE_STATUS_UPDATED":
		return disputeBody(data)
	case "CARD_3DS_CHALLENGE_CREATED":
		return challengeBody(data)
	case "CARD_STATUS_UPDATED":
		return cardStatusBody(data)
	case "ACCOUNT_STATUS_UPDATED":
		return accountStatusBody(data)
	default:
		return humanize(eventType)
	}
}

// userApplicationBody covers USER_APPLICATION_STATUS_UPDATED (user
// resource, application.status — see https://docs.reap.global/api-reference
// GET /users/:id).
func userApplicationBody(data json.RawMessage) string {
	var payload struct {
		Application struct {
			Status string `json:"status"`
		} `json:"application"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Application.Status == "" {
		return "Your KYC application has been updated"
	}
	return fmt.Sprintf("Your KYC application has been %s", humanize(payload.Application.Status))
}

// depositBody covers CRYPTO_DEPOSIT_CREATED/STATUS_UPDATED (crypto deposit
// resource: transactionId, status, amount, asset.symbol, occurredAt).
func depositBody(data json.RawMessage, verb string) string {
	var payload struct {
		TransactionID string `json:"transactionId"`
		Status        string `json:"status"`
		Amount        string `json:"amount"`
		OccurredAt    string `json:"occurredAt"`
		Asset         struct {
			Symbol string `json:"symbol"`
		} `json:"asset"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "Your deposit has been " + verb
	}
	return fmt.Sprintf("Your deposit has been %s: %s (%s). Tx %s at %s",
		verb, formatAmount(payload.Amount, payload.Asset.Symbol), humanize(payload.Status),
		payload.TransactionID, formatTimestamp(payload.OccurredAt))
}

// transactionBody covers CARD_TRANSACTION_CREATED/UPDATED (card transaction
// resource: status, amount, currency, merchant.name).
func transactionBody(data json.RawMessage, verb string) string {
	var payload struct {
		Status   string `json:"status"`
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
		Merchant struct {
			Name string `json:"name"`
		} `json:"merchant"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return verb + " card transaction"
	}
	merchant := payload.Merchant.Name
	if merchant == "" {
		merchant = "a merchant"
	}
	return fmt.Sprintf("%s card transaction: %s at %s (%s)",
		verb, formatAmount(payload.Amount, payload.Currency), merchant, humanize(payload.Status))
}

// fraudAlertBody covers CARD_FRAUD_ALERT_CREATED/STATUS_UPDATED (fraud
// alert resource: status, type — e.g. CARD_LOST, CARD_STOLEN).
func fraudAlertBody(data json.RawMessage) string {
	var payload struct {
		Status string `json:"status"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Status == "" {
		return "Fraud alert on your card"
	}
	if payload.Type == "" {
		return fmt.Sprintf("Fraud alert: %s", humanize(payload.Status))
	}
	return fmt.Sprintf("Fraud alert: %s (%s)", humanize(payload.Type), humanize(payload.Status))
}

// shipmentBody covers CARD_SHIPMENT_STATUS_UPDATED (shipment resource:
// status, trackingNumber).
func shipmentBody(data json.RawMessage) string {
	var payload struct {
		Status         string `json:"status"`
		TrackingNumber string `json:"trackingNumber"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Status == "" {
		return "Your card shipment has been updated"
	}
	body := fmt.Sprintf("Your card shipment is now %s", humanize(payload.Status))
	if payload.TrackingNumber != "" {
		body += fmt.Sprintf(" (tracking %s)", payload.TrackingNumber)
	}
	return body
}

// disputeBody covers CARD_DISPUTE_STATUS_UPDATED (dispute resource: status).
func disputeBody(data json.RawMessage) string {
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Status == "" {
		return "Your card dispute has been updated"
	}
	return fmt.Sprintf("Your card dispute is now %s", humanize(payload.Status))
}

// challengeBody covers CARD_3DS_CHALLENGE_CREATED (3DS challenge resource:
// merchant.name, transaction.amount, transaction.currency).
func challengeBody(data json.RawMessage) string {
	var payload struct {
		Merchant struct {
			Name string `json:"name"`
		} `json:"merchant"`
		Transaction struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"transaction"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "Approve a card purchase"
	}
	merchant := payload.Merchant.Name
	if merchant == "" {
		merchant = "a merchant"
	}
	return fmt.Sprintf("Approve your %s purchase at %s?",
		formatAmount(payload.Transaction.Amount, payload.Transaction.Currency), merchant)
}

// cardStatusBody covers CARD_STATUS_UPDATED (card resource: status — e.g.
// ACTIVE, FROZEN, BLOCKED, EXPIRED).
func cardStatusBody(data json.RawMessage) string {
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Status == "" {
		return "Your card status has been updated"
	}
	return fmt.Sprintf("Your card is now %s", humanize(payload.Status))
}

// accountStatusBody covers ACCOUNT_STATUS_UPDATED (account resource: status
// — e.g. ACTIVE, RESTRICTED).
func accountStatusBody(data json.RawMessage) string {
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Status == "" {
		return "Your account status has been updated"
	}
	return fmt.Sprintf("Your account is now %s", humanize(payload.Status))
}

// humanize turns a REAP enum or event-type constant (e.g.
// "CARD_STATUS_UPDATED", "IN_TRANSIT") into lowercase, space-separated text
// for a notification body.
func humanize(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", " "))
}

// formatAmount renders an amount with its currency/asset symbol for a
// notification body, e.g. formatAmount("100.00", "USDC") -> "100.00 USDC".
// REAP returns amounts as strings (arbitrary-precision decimals), so this
// never parses them as numbers.
func formatAmount(amount, unit string) string {
	switch {
	case amount == "":
		return unit
	case unit == "":
		return amount
	default:
		return amount + " " + unit
	}
}

// formatTimestamp renders an RFC3339 timestamp for a notification body,
// falling back to the raw value if it doesn't parse (or is empty).
func formatTimestamp(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.Format("Jan 2, 2006 3:04 PM")
}
