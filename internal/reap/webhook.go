package reap

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// webhookTimestampTolerance is REAP's recommended replay-protection window
// (https://docs.reap.global/webhooks/signature-verification).
const webhookTimestampTolerance = 5 * time.Minute

var ErrInvalidWebhookSignature = errors.New("reap: invalid webhook signature")

// VerifyWebhookSignature checks signatureHeader (the raw
// X-Reap-Webhook-Signature header value, formatted "t=<unix>,v1=<hex hmac>")
// against rawBody using secret, and rejects it if its timestamp is more than
// webhookTimestampTolerance away from now. rawBody must be the exact bytes
// REAP sent — re-serializing the parsed JSON changes whitespace/key order
// and breaks the signature.
func VerifyWebhookSignature(secret string, rawBody []byte, signatureHeader string, now time.Time) error {
	var timestamp, signature string
	for _, part := range strings.Split(signatureHeader, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			timestamp = value
		case "v1":
			signature = value
		}
	}
	if timestamp == "" || signature == "" {
		return ErrInvalidWebhookSignature
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrInvalidWebhookSignature
	}
	if now.Sub(time.Unix(ts, 0)).Abs() > webhookTimestampTolerance {
		return ErrInvalidWebhookSignature
	}

	sig, err := hex.DecodeString(signature)
	if err != nil {
		return ErrInvalidWebhookSignature
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "." + string(rawBody)))
	expected := mac.Sum(nil)

	if subtle.ConstantTimeCompare(expected, sig) != 1 {
		return ErrInvalidWebhookSignature
	}
	return nil
}
