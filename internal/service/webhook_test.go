package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"
)

func signWebhook(secret string, ts int64, body string) string {
	timestamp := strconv.FormatInt(ts, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "." + body))
	return "t=" + timestamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// TestHandleEventInvalidPayload covers a correctly signed webhook body that
// doesn't parse as a valid event envelope (missing id/type): HandleEvent
// must return ErrInvalidWebhookPayload, not a generic error, so the HTTP
// handler can map it to a 4xx instead of a 5xx REAP will keep retrying.
func TestHandleEventInvalidPayload(t *testing.T) {
	const secret = "test-secret"
	body := `{"type":"CARD_STATUS_UPDATED"}` // missing id
	now := time.Now()

	s := NewWebhookService(nil, secret, nil)
	err := s.HandleEvent(context.Background(), []byte(body), signWebhook(secret, now.Unix(), body))
	if !errors.Is(err, ErrInvalidWebhookPayload) {
		t.Fatalf("HandleEvent() error = %v, want ErrInvalidWebhookPayload", err)
	}
}

func TestHandleEventInvalidSignature(t *testing.T) {
	s := NewWebhookService(nil, "test-secret", nil)
	err := s.HandleEvent(context.Background(), []byte(`{"id":"evt_1","type":"CARD_STATUS_UPDATED"}`), "not-a-valid-header")
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Fatalf("HandleEvent() error = %v, want ErrInvalidWebhookSignature", err)
	}
}

func TestEventSubjectIDs(t *testing.T) {
	tests := []struct {
		name                                  string
		eventType, data                       string
		wantCardID, wantAccountID, wantUserID string
	}{
		{
			name:       "card transaction carries cardId",
			eventType:  "CARD_TRANSACTION_CREATED",
			data:       `{"id":"txn_1","cardId":"card_1"}`,
			wantCardID: "card_1",
		},
		{
			name:       "fraud alert carries cardId",
			eventType:  "CARD_FRAUD_ALERT_CREATED",
			data:       `{"id":"alert_1","cardId":"card_1"}`,
			wantCardID: "card_1",
		},
		{
			name:       "card transaction updated carries cardId",
			eventType:  "CARD_TRANSACTION_UPDATED",
			data:       `{"id":"txn_1","cardId":"card_1"}`,
			wantCardID: "card_1",
		},
		{
			name:       "fraud alert status updated carries cardId",
			eventType:  "CARD_FRAUD_ALERT_STATUS_UPDATED",
			data:       `{"id":"alert_1","cardId":"card_1"}`,
			wantCardID: "card_1",
		},
		{
			name:       "card dispute carries cardId",
			eventType:  "CARD_DISPUTE_STATUS_UPDATED",
			data:       `{"id":"dispute_1","cardId":"card_1","transactionId":"txn_1"}`,
			wantCardID: "card_1",
		},
		{
			name:       "card shipment carries cards[].cardId",
			eventType:  "CARD_SHIPMENT_STATUS_UPDATED",
			data:       `{"id":"ship_1","cards":[{"cardId":"card_1"},{"cardId":"card_2"}]}`,
			wantCardID: "card_1",
		},
		{
			name:       "card status updated is keyed by its own id",
			eventType:  "CARD_STATUS_UPDATED",
			data:       `{"id":"card_1","status":"ACTIVE"}`,
			wantCardID: "card_1",
		},
		{
			name:          "crypto deposit created carries accountId",
			eventType:     "CRYPTO_DEPOSIT_CREATED",
			data:          `{"id":"deposit_1","accountId":"account_1","chainId":"BASE","status":"PENDING","amount":"100.00"}`,
			wantAccountID: "account_1",
		},
		{
			name:          "crypto deposit status updated carries accountId",
			eventType:     "CRYPTO_DEPOSIT_STATUS_UPDATED",
			data:          `{"id":"deposit_1","accountId":"account_1","status":"CONFIRMED"}`,
			wantAccountID: "account_1",
		},
		{
			name:          "account status updated is keyed by its own id",
			eventType:     "ACCOUNT_STATUS_UPDATED",
			data:          `{"id":"account_1","status":"ACTIVE"}`,
			wantAccountID: "account_1",
		},
		{
			// REAP publishes no payload schema for this event, and its most
			// plausible source resource (the push-provisioning response) has
			// no cardId field at all — see the ponytail comment on
			// eventSubjectIDs. This documents the current (unresolved)
			// behavior rather than asserting a guessed shape.
			name:      "card tokenization requested is not resolved",
			eventType: "CARD_TOKENIZATION_REQUESTED",
			data:      `{"provider":"GOOGLE_PAY","opc":"...","last4":"1234"}`,
		},
		{
			name:       "user application status updated is keyed by its own id",
			eventType:  "USER_APPLICATION_STATUS_UPDATED",
			data:       `{"id":"user_1","status":"APPROVED"}`,
			wantUserID: "user_1",
		},
		{
			name:      "unresolvable event type",
			eventType: "COMPANY_STATUS_UPDATED",
			data:      `{"id":"company_1","status":"ACTIVE"}`,
		},
		{
			name:      "malformed data",
			eventType: "CARD_STATUS_UPDATED",
			data:      `not json`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cardID, accountID, userID := eventSubjectIDs(tt.eventType, []byte(tt.data))
			if cardID != tt.wantCardID || accountID != tt.wantAccountID || userID != tt.wantUserID {
				t.Fatalf("eventSubjectIDs() = (%q, %q, %q), want (%q, %q, %q)",
					cardID, accountID, userID, tt.wantCardID, tt.wantAccountID, tt.wantUserID)
			}
		})
	}
}

func TestNotificationBody(t *testing.T) {
	tests := []struct {
		name            string
		eventType, data string
		want            string
	}{
		{
			name:      "user application approved",
			eventType: "USER_APPLICATION_STATUS_UPDATED",
			data:      `{"application":{"status":"APPROVED"}}`,
			want:      "Your KYC application has been approved",
		},
		{
			name:      "deposit created",
			eventType: "CRYPTO_DEPOSIT_CREATED",
			data:      `{"transactionId":"tx_1","status":"PENDING","amount":"100.00","occurredAt":"2024-01-02T15:04:05Z","asset":{"symbol":"USDC"}}`,
			want:      "Your deposit has been detected: 100.00 USDC (pending). Tx tx_1 at Jan 2, 2024 3:04 PM",
		},
		{
			name:      "deposit status updated",
			eventType: "CRYPTO_DEPOSIT_STATUS_UPDATED",
			data:      `{"transactionId":"tx_1","status":"CONFIRMED","amount":"100.00","occurredAt":"2024-01-02T15:04:05Z","asset":{"symbol":"USDC"}}`,
			want:      "Your deposit has been updated: 100.00 USDC (confirmed). Tx tx_1 at Jan 2, 2024 3:04 PM",
		},
		{
			name:      "card transaction created",
			eventType: "CARD_TRANSACTION_CREATED",
			data:      `{"status":"PENDING","amount":"170.0000","currency":"USD","merchant":{"name":"Sephora Digital"}}`,
			want:      "New card transaction: 170.0000 USD at Sephora Digital (pending)",
		},
		{
			name:      "card transaction updated",
			eventType: "CARD_TRANSACTION_UPDATED",
			data:      `{"status":"CLEARED","amount":"170.0000","currency":"USD","merchant":{"name":"Sephora Digital"}}`,
			want:      "Updated card transaction: 170.0000 USD at Sephora Digital (cleared)",
		},
		{
			name:      "fraud alert created",
			eventType: "CARD_FRAUD_ALERT_CREATED",
			data:      `{"status":"PENDING","type":"CARD_STOLEN"}`,
			want:      "Fraud alert: card stolen (pending)",
		},
		{
			name:      "fraud alert status updated",
			eventType: "CARD_FRAUD_ALERT_STATUS_UPDATED",
			data:      `{"status":"CONFIRMED","type":"CARD_STOLEN"}`,
			want:      "Fraud alert: card stolen (confirmed)",
		},
		{
			name:      "shipment updated with tracking",
			eventType: "CARD_SHIPMENT_STATUS_UPDATED",
			data:      `{"status":"IN_TRANSIT","trackingNumber":"1Z999AA1"}`,
			want:      "Your card shipment is now in transit (tracking 1Z999AA1)",
		},
		{
			name:      "shipment updated without tracking",
			eventType: "CARD_SHIPMENT_STATUS_UPDATED",
			data:      `{"status":"DRAFT"}`,
			want:      "Your card shipment is now draft",
		},
		{
			name:      "dispute updated",
			eventType: "CARD_DISPUTE_STATUS_UPDATED",
			data:      `{"status":"RESOLVED"}`,
			want:      "Your card dispute is now resolved",
		},
		{
			name:      "3ds challenge",
			eventType: "CARD_3DS_CHALLENGE_CREATED",
			data:      `{"merchant":{"name":"Sephora Digital"},"transaction":{"amount":"170.0000","currency":"USD"}}`,
			want:      "Approve your 170.0000 USD purchase at Sephora Digital?",
		},
		{
			name:      "card status updated",
			eventType: "CARD_STATUS_UPDATED",
			data:      `{"id":"card_1","status":"FROZEN"}`,
			want:      "Your card is now frozen",
		},
		{
			name:      "account status updated",
			eventType: "ACCOUNT_STATUS_UPDATED",
			data:      `{"id":"account_1","status":"RESTRICTED"}`,
			want:      "Your account is now restricted",
		},
		{
			name:      "unknown event type falls back to humanized type",
			eventType: "COMPANY_STATUS_UPDATED",
			data:      `{"id":"company_1","status":"ACTIVE"}`,
			want:      "company status updated",
		},
		{
			name:      "malformed data falls back gracefully",
			eventType: "CARD_STATUS_UPDATED",
			data:      `not json`,
			want:      "Your card status has been updated",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notificationBody(tt.eventType, []byte(tt.data))
			if got != tt.want {
				t.Fatalf("notificationBody() = %q, want %q", got, tt.want)
			}
		})
	}
}
