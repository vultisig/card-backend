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
