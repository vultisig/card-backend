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

	s := NewWebhookService(nil, secret)
	err := s.HandleEvent(context.Background(), []byte(body), signWebhook(secret, now.Unix(), body))
	if !errors.Is(err, ErrInvalidWebhookPayload) {
		t.Fatalf("HandleEvent() error = %v, want ErrInvalidWebhookPayload", err)
	}
}

func TestHandleEventInvalidSignature(t *testing.T) {
	s := NewWebhookService(nil, "test-secret")
	err := s.HandleEvent(context.Background(), []byte(`{"id":"evt_1","type":"CARD_STATUS_UPDATED"}`), "not-a-valid-header")
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Fatalf("HandleEvent() error = %v, want ErrInvalidWebhookSignature", err)
	}
}
