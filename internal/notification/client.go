// Package notification is a thin client for the Vultisig notification
// service (POST /notify), used to push a client notification when a REAP
// webhook event affects a known vault.
package notification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const requestTimeout = 10 * time.Second

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: requestTimeout}}
}

// Request is the body of POST /notify. Type must be set to something other
// than "" (which the notification service defaults to "keysign" and then
// requires vault_name/qr_code_data/local_party_id for) — "generic" only
// requires VaultID.
type Request struct {
	VaultID string `json:"vault_id"`
	Type    string `json:"type"`
	Title   string `json:"title,omitempty"`
	Body    string `json:"body,omitempty"`
}

// Notify calls POST /notify. A non-2xx response is returned as an error.
func (c *Client) Notify(ctx context.Context, req Request) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/notify", bytes.NewReader(b))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notification: notify returned %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

// VaultID computes the notification service's vault_id for a card-backend
// vault: SHA256(utf8(pubKeyECDSA + hexChainCode)) as lowercase hex, matching
// the client SDKs' computeNotificationVaultId.
func VaultID(pubKeyECDSA, hexChainCode string) string {
	sum := sha256.Sum256([]byte(pubKeyECDSA + hexChainCode))
	return hex.EncodeToString(sum[:])
}
