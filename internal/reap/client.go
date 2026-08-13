// Package reap is a thin client for the REAP card-issuing API
// (https://docs.reap.global/api-reference).
package reap

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

const apiVersion = "2025-02-14"

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
	return &Client{baseURL: baseURL, apiKey: apiKey, http: http.DefaultClient}
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

func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Reap-Version", apiVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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
