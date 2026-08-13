// Package reap is a thin client for the REAP card-issuing API
// (https://docs.reap.global/api-reference).
package reap

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const apiVersion = "2025-02-14"

const requestTimeout = 10 * time.Second

const (
	EnvSandbox = "sandbox"
	EnvProd    = "prod"
)

const (
	sandboxBaseURL = "https://sandbox.api.reap.global"
	prodBaseURL    = "https://prod.api.reap.global"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient builds a REAP API client. Any env value other than EnvProd
// resolves to the sandbox base URL.
func NewClient(env, apiKey string) *Client {
	baseURL := sandboxBaseURL
	if env == EnvProd {
		baseURL = prodBaseURL
	}
	return &Client{baseURL: baseURL, apiKey: apiKey, http: &http.Client{Timeout: requestTimeout}}
}

type TermsAcceptance struct {
	Version   string `json:"version"`
	IPAddress string `json:"ipAddress"`
}

type CreateUserRequest struct {
	Email           string          `json:"email"`
	PhoneNumber     string          `json:"phoneNumber"`
	FirstName       string          `json:"firstName,omitempty"`
	LastName        string          `json:"lastName,omitempty"`
	TermsAcceptance TermsAcceptance `json:"termsAcceptance"`
}

// CreateUser calls POST /users/. It returns REAP's raw JSON response body
// and status code as-is (including non-2xx error bodies) so callers can
// pass them straight through to their own client.
func (c *Client) CreateUser(ctx context.Context, req CreateUserRequest) (status int, body []byte, err error) {
	b, err := json.Marshal(req)
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, "/users/", bytes.NewReader(b))
}

// GetUser calls GET /users/{id}. It returns REAP's raw JSON response body
// and status code as-is (including non-2xx error bodies) so callers can
// pass them straight through to their own client.
func (c *Client) GetUser(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, "/users/"+id, nil)
}

// UpdateEmail calls PUT /users/{id}/email. On success REAP returns 204 with
// an empty body; on error it returns REAP's raw JSON error body as-is.
func (c *Client) UpdateEmail(ctx context.Context, id, email string) (status int, body []byte, err error) {
	b, err := json.Marshal(struct {
		Email string `json:"email"`
	}{email})
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPut, "/users/"+id+"/email", bytes.NewReader(b))
}

// UpdatePhoneNumber calls PUT /users/{id}/phone. On success REAP returns 204
// with an empty body; on error it returns REAP's raw JSON error body as-is.
func (c *Client) UpdatePhoneNumber(ctx context.Context, id, phoneNumber string) (status int, body []byte, err error) {
	b, err := json.Marshal(struct {
		PhoneNumber string `json:"phoneNumber"`
	}{phoneNumber})
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPut, "/users/"+id+"/phone", bytes.NewReader(b))
}

// Signer is a client-signed authorization to act on behalf of an account
// owner, as returned by signing the message from GenerateSignerMessage.
type Signer struct {
	Address   string `json:"address"`
	Message   string `json:"message"`
	Signature string `json:"signature"`
}

type Signers struct {
	EVM *Signer `json:"evm,omitempty"`
	SVM *Signer `json:"svm,omitempty"`
}

type CreateAccountRequest struct {
	OwnerID string   `json:"ownerId"`
	Signers *Signers `json:"signers,omitempty"`
}

// CreateAccount calls POST /accounts/, forwarding idempotencyKey as the
// Idempotency-Key header verbatim (REAP requires it; generating it is the
// caller's responsibility, not this client's). It returns REAP's raw JSON
// response body and status code as-is (including non-2xx error bodies) so
// callers can pass them straight through to their own client.
func (c *Client) CreateAccount(ctx context.Context, req CreateAccountRequest, idempotencyKey string) (status int, body []byte, err error) {
	b, err := json.Marshal(req)
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, "/accounts/", bytes.NewReader(b), func(req *http.Request) {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	})
}

// GenerateSignerMessage calls GET /accounts/auth-message. It returns REAP's
// raw JSON response body and status code as-is.
func (c *Client) GenerateSignerMessage(ctx context.Context) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, "/accounts/auth-message", nil)
}

// GetAccount calls GET /accounts/{id}. It returns REAP's raw JSON response
// body and status code as-is (including non-2xx error bodies) so callers can
// pass them straight through to their own client.
func (c *Client) GetAccount(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, "/accounts/"+id, nil)
}

// GetAccountBalance calls GET /accounts/{id}/balance. It returns REAP's raw
// JSON response body and status code as-is.
func (c *Client) GetAccountBalance(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, "/accounts/"+id+"/balance", nil)
}

// GetAccountAssets calls GET /accounts/{id}/assets. It returns REAP's raw
// JSON response body and status code as-is.
func (c *Client) GetAccountAssets(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, "/accounts/"+id+"/assets", nil)
}

// ListAccounts calls GET /accounts/, filtered to ownerID. limit <= 0 and an
// empty cursor are omitted, letting REAP apply its own defaults. It returns
// REAP's raw JSON response body and status code as-is.
func (c *Client) ListAccounts(ctx context.Context, ownerID string, limit int, cursor string) (status int, body []byte, err error) {
	q := url.Values{"ownerId": {ownerID}}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	return c.do(ctx, http.MethodGet, "/accounts/?"+q.Encode(), nil)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, opts ...func(*http.Request)) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Reap-Version", apiVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, opt := range opts {
		opt(req)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, respBody, nil
}
