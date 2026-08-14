package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/cardownership"
	"github.com/vultisig/card-backend/internal/reap"
)

// FraudAlertService backs /fraud-alerts. Fraud alerts and card transactions
// carry a cardId REAP field, so every method resolves it (fetching the
// alert or transaction first if the caller didn't supply a card ID
// directly) and checks it against cardownership before acting, scoping
// every method to cards the calling vault created.
type FraudAlertService struct {
	pool *pgxpool.Pool
	reap *reap.Client
}

func NewFraudAlertService(pool *pgxpool.Pool, reapClient *reap.Client) *FraudAlertService {
	return &FraudAlertService{pool: pool, reap: reapClient}
}

// transactionCardID fetches transactionID's card ID. On a non-2xx REAP
// response it returns that status/body unchanged (err nil, cardID "") for
// passthrough.
func (s *FraudAlertService) transactionCardID(ctx context.Context, transactionID string) (status int, body []byte, cardID string, err error) {
	status, body, err = s.reap.GetCardTransaction(ctx, transactionID)
	if err != nil {
		return 0, nil, "", err
	}
	if status < 200 || status >= 300 {
		return status, body, "", nil
	}
	var txn struct {
		CardID string `json:"cardId"`
	}
	if err := json.Unmarshal(body, &txn); err != nil || txn.CardID == "" {
		return 0, nil, "", errors.New("reap: get card transaction response missing cardId")
	}
	return status, body, txn.CardID, nil
}

// alertCardID fetches alertID's card ID. On a non-2xx REAP response it
// returns that status/body unchanged (err nil, cardID "") for passthrough.
func (s *FraudAlertService) alertCardID(ctx context.Context, alertID string) (status int, body []byte, cardID string, err error) {
	status, body, err = s.reap.GetFraudAlert(ctx, alertID)
	if err != nil {
		return 0, nil, "", err
	}
	if status < 200 || status >= 300 {
		return status, body, "", nil
	}
	var alert struct {
		CardID string `json:"cardId"`
	}
	if err := json.Unmarshal(body, &alert); err != nil || alert.CardID == "" {
		return 0, nil, "", errors.New("reap: get fraud alert response missing cardId")
	}
	return status, body, alert.CardID, nil
}

func (s *FraudAlertService) checkCardOwnership(ctx context.Context, publicKey, cardID string) error {
	owned, err := cardownership.IsOwner(ctx, s.pool, publicKey, cardID)
	if err != nil {
		return err
	}
	if !owned {
		return ErrResourceNotOwned
	}
	return nil
}

// ListFraudAlerts requires a cardId or transactionId filter (REAP's only
// filters that identify a specific card) and checks it against
// cardownership, so a vault can't list every card's fraud alerts by
// omitting filters. It returns ErrMissingScopeFilter if neither is set.
func (s *FraudAlertService) ListFraudAlerts(ctx context.Context, publicKey string, query url.Values) (status int, body []byte, err error) {
	cardID := query.Get("cardId")
	if cardID == "" {
		transactionID := query.Get("transactionId")
		if transactionID == "" {
			return 0, nil, ErrMissingScopeFilter
		}
		status, body, cardID, err = s.transactionCardID(ctx, transactionID)
		if err != nil || cardID == "" {
			return status, body, err
		}
	}
	if err := s.checkCardOwnership(ctx, publicKey, cardID); err != nil {
		return 0, nil, err
	}
	// Forward only the cardId just checked, dropping transactionId (already
	// resolved above) and collapsing any repeated cardId values, so a
	// caller can't sneak an extra/different value past the ownership check
	// via a second query param occurrence.
	query.Del("transactionId")
	query.Set("cardId", cardID)
	return s.reap.ListFraudAlerts(ctx, query)
}

// ReportFraud resolves req.TransactionID's card and checks it against
// cardownership before reporting fraud on it.
func (s *FraudAlertService) ReportFraud(ctx context.Context, publicKey string, req reap.ReportFraudRequest, idempotencyKey string) (status int, body []byte, err error) {
	status, body, cardID, err := s.transactionCardID(ctx, req.TransactionID)
	if err != nil || cardID == "" {
		return status, body, err
	}
	if err := s.checkCardOwnership(ctx, publicKey, cardID); err != nil {
		return 0, nil, err
	}
	return s.reap.ReportFraud(ctx, req, idempotencyKey)
}

// GetFraudAlert checks the alert's card against cardownership before
// returning it.
func (s *FraudAlertService) GetFraudAlert(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	status, body, cardID, err := s.alertCardID(ctx, id)
	if err != nil || cardID == "" {
		return status, body, err
	}
	if err := s.checkCardOwnership(ctx, publicKey, cardID); err != nil {
		return 0, nil, err
	}
	return status, body, nil
}

// RespondToFraudAlert checks the alert's card against cardownership before
// responding to it.
func (s *FraudAlertService) RespondToFraudAlert(ctx context.Context, publicKey, id string, req reap.RespondToFraudAlertRequest, idempotencyKey string) (status int, body []byte, err error) {
	status, body, cardID, err := s.alertCardID(ctx, id)
	if err != nil || cardID == "" {
		return status, body, err
	}
	if err := s.checkCardOwnership(ctx, publicKey, cardID); err != nil {
		return 0, nil, err
	}
	return s.reap.RespondToFraudAlert(ctx, id, req, idempotencyKey)
}
