// Package reap is a thin client for the REAP card-issuing API
// (https://docs.reap.global/api-reference).
package reap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vultisig/card-backend/internal/statsd"
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
	stats   *statsd.Client
}

// NewClient builds a REAP API client. Any env value other than EnvProd
// resolves to the sandbox base URL. stats may be nil (metrics are a no-op).
func NewClient(env, apiKey string, stats *statsd.Client) *Client {
	baseURL := sandboxBaseURL
	if env == EnvProd {
		baseURL = prodBaseURL
	}
	return &Client{baseURL: baseURL, apiKey: apiKey, http: &http.Client{Timeout: requestTimeout}, stats: stats}
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
	return c.do(ctx, http.MethodGet, pathf("/users/%s", id), nil)
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
	return c.do(ctx, http.MethodPut, pathf("/users/%s/email", id), bytes.NewReader(b))
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
	return c.do(ctx, http.MethodPut, pathf("/users/%s/phone", id), bytes.NewReader(b))
}

// AdvanceUserApplication calls POST /users/{id}/application, forwarding
// idempotencyKey as the Idempotency-Key header when set (REAP treats it as
// optional here). The request body shape varies by KYC method
// (MANAGED_KYC/SUMSUB_TOKEN_SHARING/UNIVERSAL_KYC — the latter deeply
// nested), so body is forwarded to REAP unchanged rather than through a
// typed struct. It returns REAP's raw JSON response body and status code
// as-is.
func (c *Client) AdvanceUserApplication(ctx context.Context, id string, body []byte, idempotencyKey string) (status int, respBody []byte, err error) {
	return c.do(ctx, http.MethodPost, pathf("/users/%s/application", id), bytes.NewReader(body), func(r *http.Request) {
		if idempotencyKey != "" {
			r.Header.Set("Idempotency-Key", idempotencyKey)
		}
	})
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
	return c.do(ctx, http.MethodGet, pathf("/accounts/%s", id), nil)
}

// GetAccountBalance calls GET /accounts/{id}/balance. It returns REAP's raw
// JSON response body and status code as-is.
func (c *Client) GetAccountBalance(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, pathf("/accounts/%s/balance", id), nil)
}

// GetAccountAssets calls GET /accounts/{id}/assets. It returns REAP's raw
// JSON response body and status code as-is.
func (c *Client) GetAccountAssets(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, pathf("/accounts/%s/assets", id), nil)
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

type CreateCardRequest struct {
	UserID                 string `json:"userId"`
	AccountID              string `json:"accountId"`
	Type                   string `json:"type"`
	ThreeDSChallengeMethod string `json:"3dsChallengeMethod,omitempty"`
	CardDesignID           string `json:"cardDesignId,omitempty"`
}

// CreateCard calls POST /cards/, forwarding idempotencyKey as the
// Idempotency-Key header verbatim (REAP requires it; generating it is the
// caller's responsibility, not this client's). It returns REAP's raw JSON
// response body and status code as-is (including non-2xx error bodies) so
// callers can pass them straight through to their own client.
func (c *Client) CreateCard(ctx context.Context, req CreateCardRequest, idempotencyKey string) (status int, body []byte, err error) {
	b, err := json.Marshal(req)
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, "/cards/", bytes.NewReader(b), func(r *http.Request) {
		r.Header.Set("Idempotency-Key", idempotencyKey)
	})
}

// ListCards calls GET /cards/ with query forwarded as-is; the caller is
// responsible for setting/forcing any filters (e.g. userId) before calling.
// It returns REAP's raw JSON response body and status code as-is.
func (c *Client) ListCards(ctx context.Context, query url.Values) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, "/cards/?"+query.Encode(), nil)
}

// GetCard calls GET /cards/{id}. It returns REAP's raw JSON response body
// and status code as-is (including non-2xx error bodies).
func (c *Client) GetCard(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, pathf("/cards/%s", id), nil)
}

// DeleteCard calls DELETE /cards/{id}. On success REAP returns 204 with an
// empty body; on error it returns REAP's raw JSON error body as-is.
func (c *Client) DeleteCard(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodDelete, pathf("/cards/%s", id), nil)
}

// UpdateCardPin calls PUT /cards/{id}/pin. On success REAP returns 204 with
// an empty body; on error it returns REAP's raw JSON error body as-is.
func (c *Client) UpdateCardPin(ctx context.Context, id, pin string) (status int, body []byte, err error) {
	b, err := json.Marshal(struct {
		Pin string `json:"pin"`
	}{pin})
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPut, pathf("/cards/%s/pin", id), bytes.NewReader(b))
}

// FreezeCard calls POST /cards/{id}/freeze. It returns REAP's raw JSON
// response body and status code as-is.
func (c *Client) FreezeCard(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodPost, pathf("/cards/%s/freeze", id), nil)
}

// UnfreezeCard calls POST /cards/{id}/unfreeze. It returns REAP's raw JSON
// response body and status code as-is.
func (c *Client) UnfreezeCard(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodPost, pathf("/cards/%s/unfreeze", id), nil)
}

// RevealCardDetails calls POST /cards/{id}/reveal. The returned revealUrl is
// valid for 5 minutes; callers must not store, log, or expose it beyond the
// immediate display context.
func (c *Client) RevealCardDetails(ctx context.Context, id, stylesheetURL string, showCopyPanButton bool) (status int, body []byte, err error) {
	b, err := json.Marshal(struct {
		StylesheetURL     string `json:"stylesheetUrl,omitempty"`
		ShowCopyPanButton bool   `json:"showCopyPanButton"`
	}{stylesheetURL, showCopyPanButton})
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, pathf("/cards/%s/reveal", id), bytes.NewReader(b))
}

// UpdateCard3DSChallengeMethod calls PUT /cards/{id}/3ds-challenge-method.
// On success REAP returns 204 with an empty body; on error it returns
// REAP's raw JSON error body as-is.
func (c *Client) UpdateCard3DSChallengeMethod(ctx context.Context, id, method string) (status int, body []byte, err error) {
	b, err := json.Marshal(struct {
		ThreeDSChallengeMethod string `json:"3dsChallengeMethod"`
	}{method})
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPut, pathf("/cards/%s/3ds-challenge-method", id), bytes.NewReader(b))
}

// ActivatePhysicalCard calls POST /cards/{id}/activate. On success REAP
// returns 204 with an empty body; on error it returns REAP's raw JSON error
// body as-is.
func (c *Client) ActivatePhysicalCard(ctx context.Context, id, activationCode string) (status int, body []byte, err error) {
	b, err := json.Marshal(struct {
		ActivationCode string `json:"activationCode"`
	}{activationCode})
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, pathf("/cards/%s/activate", id), bytes.NewReader(b))
}

// GetCardActivationCode calls GET /cards/{id}/activation-code. It returns
// REAP's raw JSON response body and status code as-is.
func (c *Client) GetCardActivationCode(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, pathf("/cards/%s/activation-code", id), nil)
}

// PushProvisionCard calls POST /cards/{id}/push-provisioning, forwarding
// idempotencyKey as the Idempotency-Key header when set (REAP treats it as
// optional on this endpoint). It returns REAP's raw JSON response body and
// status code as-is.
func (c *Client) PushProvisionCard(ctx context.Context, id, provider, walletAccountID, deviceID, idempotencyKey string) (status int, body []byte, err error) {
	b, err := json.Marshal(struct {
		Provider        string `json:"provider"`
		WalletAccountID string `json:"walletAccountId"`
		DeviceID        string `json:"deviceId"`
	}{provider, walletAccountID, deviceID})
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, pathf("/cards/%s/push-provisioning", id), bytes.NewReader(b), func(r *http.Request) {
		if idempotencyKey != "" {
			r.Header.Set("Idempotency-Key", idempotencyKey)
		}
	})
}

// RespondToCard3DSChallenge calls POST /card-3ds-challenges/{id}/respond. It
// returns REAP's raw JSON response body and status code as-is.
func (c *Client) RespondToCard3DSChallenge(ctx context.Context, id string, approve bool) (status int, body []byte, err error) {
	b, err := json.Marshal(struct {
		Approve bool `json:"approve"`
	}{approve})
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, pathf("/card-3ds-challenges/%s/respond", id), bytes.NewReader(b))
}

// GetCard3DSChallenge calls GET /card-3ds-challenges/{id}. It returns REAP's
// raw JSON response body and status code as-is.
func (c *Client) GetCard3DSChallenge(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, pathf("/card-3ds-challenges/%s", id), nil)
}

type CardShippingAddress struct {
	Line1      string `json:"line1"`
	Line2      string `json:"line2,omitempty"`
	Zone       string `json:"zone,omitempty"`
	City       string `json:"city"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
}

type CardShipmentRecipient struct {
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	PhoneNumber string `json:"phoneNumber"`
	DialCode    int    `json:"dialCode"`
	Email       string `json:"email"`
}

type CardShipmentCourier struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type CardShipmentMember struct {
	CardID          string              `json:"cardId"`
	CardDesignID    string              `json:"cardDesignId"`
	ShippingAddress CardShippingAddress `json:"shippingAddress"`
}

type CreateCardShipmentRequest struct {
	DestinationAddress CardShippingAddress   `json:"destinationAddress"`
	Recipient          CardShipmentRecipient `json:"recipient"`
	Courier            CardShipmentCourier   `json:"courier"`
	Cards              []CardShipmentMember  `json:"cards"`
}

type UpdateCardShipmentRequest struct {
	DestinationAddress CardShippingAddress   `json:"destinationAddress"`
	Recipient          CardShipmentRecipient `json:"recipient"`
	Courier            CardShipmentCourier   `json:"courier"`
}

// CreateCardShipment calls POST /card-shipments/, creating a shipment in
// DRAFT status. It returns REAP's raw JSON response body and status code
// as-is.
func (c *Client) CreateCardShipment(ctx context.Context, req CreateCardShipmentRequest) (status int, body []byte, err error) {
	b, err := json.Marshal(req)
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, "/card-shipments/", bytes.NewReader(b))
}

// ListCardShipments calls GET /card-shipments/ with query forwarded as-is.
// It returns REAP's raw JSON response body and status code as-is.
func (c *Client) ListCardShipments(ctx context.Context, query url.Values) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, "/card-shipments/?"+query.Encode(), nil)
}

// GetCardShipment calls GET /card-shipments/{id}. It returns REAP's raw
// JSON response body and status code as-is.
func (c *Client) GetCardShipment(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, pathf("/card-shipments/%s", id), nil)
}

// DeleteCardShipment calls DELETE /card-shipments/{id}. Only DRAFT
// shipments can be deleted. On success REAP returns 204 with an empty body;
// on error it returns REAP's raw JSON error body as-is.
func (c *Client) DeleteCardShipment(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodDelete, pathf("/card-shipments/%s", id), nil)
}

// UpdateCardShipment calls PATCH /card-shipments/{id}, updating the
// box-level destination, recipient, or courier on a DRAFT shipment. It
// returns REAP's raw JSON response body and status code as-is.
func (c *Client) UpdateCardShipment(ctx context.Context, id string, req UpdateCardShipmentRequest) (status int, body []byte, err error) {
	b, err := json.Marshal(req)
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPatch, pathf("/card-shipments/%s", id), bytes.NewReader(b))
}

// AddCardToShipment calls POST /card-shipments/{id}/cards, appending a card
// to a DRAFT shipment. It returns REAP's raw JSON response body and status
// code as-is.
func (c *Client) AddCardToShipment(ctx context.Context, id string, member CardShipmentMember) (status int, body []byte, err error) {
	b, err := json.Marshal(member)
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, pathf("/card-shipments/%s/cards", id), bytes.NewReader(b))
}

// RemoveCardFromShipment calls DELETE /card-shipments/{id}/cards/{memberId},
// removing a card from a DRAFT shipment. It returns REAP's raw JSON
// response body and status code as-is.
func (c *Client) RemoveCardFromShipment(ctx context.Context, id, memberID string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodDelete, pathf("/card-shipments/%s/cards/%s", id, memberID), nil)
}

// SubmitCardShipment calls POST /card-shipments/{id}/submit, forwarding
// idempotencyKey as the Idempotency-Key header verbatim (REAP requires it;
// generating it is the caller's responsibility). It transitions the
// shipment from DRAFT to PLACED. It returns REAP's raw JSON response body
// and status code as-is (REAP returns 207 when some cards in the shipment
// are rejected).
func (c *Client) SubmitCardShipment(ctx context.Context, id, idempotencyKey string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodPost, pathf("/card-shipments/%s/submit", id), nil, func(r *http.Request) {
		r.Header.Set("Idempotency-Key", idempotencyKey)
	})
}

// ListCardDesigns calls GET /card-designs/ with query forwarded as-is. It
// returns REAP's raw JSON response body and status code as-is.
func (c *Client) ListCardDesigns(ctx context.Context, query url.Values) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, "/card-designs/?"+query.Encode(), nil)
}

// GetCardDesign calls GET /card-designs/{id}. It returns REAP's raw JSON
// response body and status code as-is.
func (c *Client) GetCardDesign(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, pathf("/card-designs/%s", id), nil)
}

// GetCardTransaction calls GET /card-transactions/{id}. It returns REAP's
// raw JSON response body and status code as-is.
func (c *Client) GetCardTransaction(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, pathf("/card-transactions/%s", id), nil)
}

// ListActivities calls GET /activities/ with query forwarded as-is. It
// returns REAP's raw JSON response body and status code as-is.
func (c *Client) ListActivities(ctx context.Context, query url.Values) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, "/activities/?"+query.Encode(), nil)
}

type ReportFraudRequest struct {
	TransactionID        string `json:"transactionId"`
	Type                 string `json:"type"`
	CardholderNotifiedAt string `json:"cardholderNotifiedAt,omitempty"`
}

type RespondToFraudAlertRequest struct {
	Response string `json:"response"`
	Type     string `json:"type,omitempty"`
}

// ListFraudAlerts calls GET /fraud-alerts/ with query forwarded as-is. It
// returns REAP's raw JSON response body and status code as-is.
func (c *Client) ListFraudAlerts(ctx context.Context, query url.Values) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, "/fraud-alerts/?"+query.Encode(), nil)
}

// ReportFraud calls POST /fraud-alerts/, forwarding idempotencyKey as the
// Idempotency-Key header when set (REAP treats it as optional here). It
// returns REAP's raw JSON response body and status code as-is.
func (c *Client) ReportFraud(ctx context.Context, req ReportFraudRequest, idempotencyKey string) (status int, body []byte, err error) {
	b, err := json.Marshal(req)
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, "/fraud-alerts/", bytes.NewReader(b), func(r *http.Request) {
		if idempotencyKey != "" {
			r.Header.Set("Idempotency-Key", idempotencyKey)
		}
	})
}

// GetFraudAlert calls GET /fraud-alerts/{id}. It returns REAP's raw JSON
// response body and status code as-is.
func (c *Client) GetFraudAlert(ctx context.Context, id string) (status int, body []byte, err error) {
	return c.do(ctx, http.MethodGet, pathf("/fraud-alerts/%s", id), nil)
}

// RespondToFraudAlert calls POST /fraud-alerts/{id}/respond, forwarding
// idempotencyKey as the Idempotency-Key header when set (REAP treats it as
// optional here). It returns REAP's raw JSON response body and status code
// as-is.
func (c *Client) RespondToFraudAlert(ctx context.Context, id string, req RespondToFraudAlertRequest, idempotencyKey string) (status int, body []byte, err error) {
	b, err := json.Marshal(req)
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, pathf("/fraud-alerts/%s/respond", id), bytes.NewReader(b), func(r *http.Request) {
		if idempotencyKey != "" {
			r.Header.Set("Idempotency-Key", idempotencyKey)
		}
	})
}

// SimulateUserApplicationStatus calls POST /simulation/users/{userId}/application
// (sandbox-only). On success REAP returns 204 with an empty body; on error
// it returns REAP's raw JSON error body as-is. The change applies
// asynchronously via REAP's webhook pipeline, so callers should poll
// GET /users/{id} until the desired status is observed.
func (c *Client) SimulateUserApplicationStatus(ctx context.Context, userID, targetStatus string) (status int, body []byte, err error) {
	b, err := json.Marshal(struct {
		Status string `json:"status"`
	}{targetStatus})
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, pathf("/simulation/users/%s/application", userID), bytes.NewReader(b))
}

type CompanyAddress struct {
	Line1      string `json:"line1"`
	Line2      string `json:"line2,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
	Country    string `json:"country"`
}

type CompanyApplicationDetails struct {
	LegalEntityName    string         `json:"legalEntityName"`
	RegistrationNumber string         `json:"registrationNumber"`
	Country            string         `json:"country"`
	RegisteredAddress  CompanyAddress `json:"registeredAddress"`
	OperationalAddress CompanyAddress `json:"operationalAddress"`
}

// SimulateCompanyStatusRequest's ApplicationDetails is only used (and
// required by REAP) when Status is ACTIVE.
type SimulateCompanyStatusRequest struct {
	Status             string                     `json:"status"`
	ApplicationDetails *CompanyApplicationDetails `json:"applicationDetails,omitempty"`
}

// SimulateCompanyStatus calls POST /simulation/companies/{id}/status
// (sandbox-only). On success REAP returns 204 with an empty body; on error
// it returns REAP's raw JSON error body as-is.
func (c *Client) SimulateCompanyStatus(ctx context.Context, companyID string, req SimulateCompanyStatusRequest) (status int, body []byte, err error) {
	b, err := json.Marshal(req)
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, pathf("/simulation/companies/%s/status", companyID), bytes.NewReader(b))
}

// SimulateAccountStatus calls POST /simulation/accounts/{id}/status
// (sandbox-only, targetStatus one of ACTIVE/RESTRICTED). On success REAP
// returns 204 with an empty body; on error it returns REAP's raw JSON error
// body as-is.
func (c *Client) SimulateAccountStatus(ctx context.Context, id, targetStatus string) (status int, body []byte, err error) {
	b, err := json.Marshal(struct {
		Status string `json:"status"`
	}{targetStatus})
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, pathf("/simulation/accounts/%s/status", id), bytes.NewReader(b))
}

// SimulateCardStatus calls POST /simulation/cards/{id}/status (sandbox-only,
// targetStatus one of ACTIVE/FROZEN/BLOCKED/EXPIRED). On success REAP
// returns 204 with an empty body; on error it returns REAP's raw JSON error
// body as-is.
func (c *Client) SimulateCardStatus(ctx context.Context, id, targetStatus string) (status int, body []byte, err error) {
	b, err := json.Marshal(struct {
		Status string `json:"status"`
	}{targetStatus})
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, pathf("/simulation/cards/%s/status", id), bytes.NewReader(b))
}

type SimulationMerchant struct {
	Name        string `json:"name,omitempty"`
	City        string `json:"city,omitempty"`
	PostCode    string `json:"postCode,omitempty"`
	State       string `json:"state,omitempty"`
	Country     string `json:"country,omitempty"`
	MCCCode     string `json:"mccCode,omitempty"`
	MCCCategory string `json:"mccCategory,omitempty"`
}

// SimulateCardTransactionRequest is the shared request shape for the six
// card-transaction simulation endpoints below. Not every field applies to
// every endpoint (e.g. Channel/DigitalWallet/Merchant/DeclineReason don't
// apply to clearing/reversal/refund) — omitempty means unused fields are
// simply left off the wire, so REAP applies its own defaults for them.
type SimulateCardTransactionRequest struct {
	CardID           string              `json:"cardId"`
	Amount           float64             `json:"amount"`
	TransactionID    string              `json:"transactionId,omitempty"`
	OriginalAmount   float64             `json:"originalAmount,omitempty"`
	OriginalCurrency string              `json:"originalCurrency,omitempty"`
	Channel          string              `json:"channel,omitempty"`
	DigitalWallet    string              `json:"digitalWallet,omitempty"`
	DeclineReason    string              `json:"declineReason,omitempty"`
	Merchant         *SimulationMerchant `json:"merchant,omitempty"`
}

// SimulateAuthorization calls POST /simulation/card-transactions/authorization
// (sandbox-only). It returns REAP's raw JSON response body and status code
// as-is.
func (c *Client) SimulateAuthorization(ctx context.Context, req SimulateCardTransactionRequest) (status int, body []byte, err error) {
	return c.simulateCardTransaction(ctx, "/simulation/card-transactions/authorization", req)
}

// SimulateThreeDSAuthorization calls
// POST /simulation/card-transactions/authorization-with-3ds (sandbox-only),
// simulating a WEBHOOK 3DS challenge for a card authorization. It returns
// REAP's raw JSON response body and status code as-is.
func (c *Client) SimulateThreeDSAuthorization(ctx context.Context, req SimulateCardTransactionRequest) (status int, body []byte, err error) {
	return c.simulateCardTransaction(ctx, "/simulation/card-transactions/authorization-with-3ds", req)
}

// SimulateDecline calls POST /simulation/card-transactions/decline
// (sandbox-only). It returns REAP's raw JSON response body and status code
// as-is.
func (c *Client) SimulateDecline(ctx context.Context, req SimulateCardTransactionRequest) (status int, body []byte, err error) {
	return c.simulateCardTransaction(ctx, "/simulation/card-transactions/decline", req)
}

// SimulateClearing calls POST /simulation/card-transactions/clearing
// (sandbox-only). It returns REAP's raw JSON response body and status code
// as-is.
func (c *Client) SimulateClearing(ctx context.Context, req SimulateCardTransactionRequest) (status int, body []byte, err error) {
	return c.simulateCardTransaction(ctx, "/simulation/card-transactions/clearing", req)
}

// SimulateReversal calls POST /simulation/card-transactions/reversal
// (sandbox-only). It returns REAP's raw JSON response body and status code
// as-is.
func (c *Client) SimulateReversal(ctx context.Context, req SimulateCardTransactionRequest) (status int, body []byte, err error) {
	return c.simulateCardTransaction(ctx, "/simulation/card-transactions/reversal", req)
}

// SimulateRefund calls POST /simulation/card-transactions/refund
// (sandbox-only). It returns REAP's raw JSON response body and status code
// as-is.
func (c *Client) SimulateRefund(ctx context.Context, req SimulateCardTransactionRequest) (status int, body []byte, err error) {
	return c.simulateCardTransaction(ctx, "/simulation/card-transactions/refund", req)
}

func (c *Client) simulateCardTransaction(ctx context.Context, path string, req SimulateCardTransactionRequest) (status int, body []byte, err error) {
	b, err := json.Marshal(req)
	if err != nil {
		return 0, nil, err
	}
	return c.do(ctx, http.MethodPost, path, bytes.NewReader(b))
}

// pathf builds a request path, URL-escaping every segment. Always use it for
// caller-supplied ids instead of concatenating: an unescaped "?" or "/" lets
// the caller steer a request we send under the project-wide API key.
func pathf(format string, segments ...string) string {
	escaped := make([]any, len(segments))
	for i, seg := range segments {
		escaped[i] = url.PathEscape(seg)
	}
	return fmt.Sprintf(format, escaped...)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, opts ...func(*http.Request)) (status int, respBody []byte, err error) {
	start := time.Now()
	defer func() {
		tags := []string{"method:" + method, "path:" + metricPath(path), "status:" + strconv.Itoa(status)}
		c.stats.Count("reap.request.count", 1, tags...)
		c.stats.Timing("reap.request.duration", time.Since(start), tags...)
	}()

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

	respBody, err = io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, respBody, nil
}

// metricPath collapses REAP resource IDs in path to ":id" for use as a
// metric tag: REAP IDs are UUIDs/alphanumeric and always contain a digit,
// while static path segments (accounts, cards, balance, freeze, ...) never
// do — except REAP's own literal "3ds" term, stripped before the check —
// so this keeps tag cardinality bounded without a per-call route table.
// Any query string is dropped entirely, for the same reason.
func metricPath(path string) string {
	path, _, _ = strings.Cut(path, "?")
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if strings.ContainsAny(strings.ReplaceAll(seg, "3ds", ""), "0123456789") {
			segments[i] = ":id"
		}
	}
	return strings.Join(segments, "/")
}
