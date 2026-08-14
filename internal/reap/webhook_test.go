package reap

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"
	"time"
)

func sign(secret string, ts int64, body string) string {
	timestamp := strconv.FormatInt(ts, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "." + body))
	return "t=" + timestamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhookSignature(t *testing.T) {
	const secret = "test-secret"
	body := []byte(`{"id":"evt_1","type":"CARD_STATUS_UPDATED"}`)
	now := time.Unix(1_700_000_000, 0)

	tests := []struct {
		name    string
		header  string
		body    []byte
		wantErr bool
	}{
		{"valid", sign(secret, now.Unix(), string(body)), body, false},
		{"wrong secret", sign("other-secret", now.Unix(), string(body)), body, true},
		{"tampered body", sign(secret, now.Unix(), string(body)), []byte(`{"id":"evt_2"}`), true},
		{"stale timestamp", sign(secret, now.Add(-10*time.Minute).Unix(), string(body)), body, true},
		{"future timestamp", sign(secret, now.Add(10*time.Minute).Unix(), string(body)), body, true},
		{"malformed header", "not-a-valid-header", body, true},
		{"missing v1", fmt.Sprintf("t=%d", now.Unix()), body, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyWebhookSignature(secret, tt.body, tt.header, now)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyWebhookSignature() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
