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
			name:          "account status updated is keyed by its own id",
			eventType:     "ACCOUNT_STATUS_UPDATED",
			data:          `{"id":"account_1","status":"ACTIVE"}`,
			wantAccountID: "account_1",
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
