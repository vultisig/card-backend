package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/accountownership"
	"github.com/vultisig/card-backend/internal/cardownership"
	"github.com/vultisig/card-backend/internal/reap"
)

// SimulationService backs /simulation, wrapping REAP's sandbox-only test
// helpers for advancing application/company/account/card status and
// simulating card-transaction lifecycle events.
type SimulationService struct {
	pool *pgxpool.Pool
	reap *reap.Client
}

func NewSimulationService(pool *pgxpool.Pool, reapClient *reap.Client) *SimulationService {
	return &SimulationService{pool: pool, reap: reapClient}
}

func (s *SimulationService) checkCardOwnership(ctx context.Context, publicKey, cardID string) error {
	owned, err := cardownership.IsOwner(ctx, s.pool, publicKey, cardID)
	if err != nil {
		return err
	}
	if !owned {
		return ErrResourceNotOwned
	}
	return nil
}

func (s *SimulationService) checkAccountOwnership(ctx context.Context, publicKey, accountID string) error {
	owned, err := accountownership.IsOwner(ctx, s.pool, publicKey, accountID)
	if err != nil {
		return err
	}
	if !owned {
		return ErrResourceNotOwned
	}
	return nil
}

func (s *SimulationService) checkTransactionOwnership(ctx context.Context, publicKey, transactionID string) error {
	if transactionID == "" {
		return nil
	}
	status, body, err := s.reap.GetCardTransaction(ctx, transactionID)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return ErrResourceNotOwned
	}
	var txn struct {
		CardID string `json:"cardId"`
	}
	if err := json.Unmarshal(body, &txn); err != nil || txn.CardID == "" {
		return errors.New("reap: get card transaction response missing cardId")
	}
	return s.checkCardOwnership(ctx, publicKey, txn.CardID)
}

func (s *SimulationService) SimulateUserApplicationStatus(ctx context.Context, publicKey, userID, status string) (statusCode int, body []byte, err error) {
	reapUserID, err := resolveReapUserID(ctx, s.pool, publicKey)
	if err != nil {
		return 0, nil, err
	}
	if reapUserID != userID {
		return 0, nil, ErrResourceNotOwned
	}
	return s.reap.SimulateUserApplicationStatus(ctx, userID, status)
}

func (s *SimulationService) SimulateCompanyStatus(ctx context.Context, companyID string, req reap.SimulateCompanyStatusRequest) (statusCode int, body []byte, err error) {
	return 0, nil, ErrResourceNotOwned
}

func (s *SimulationService) SimulateAccountStatus(ctx context.Context, publicKey, id, status string) (statusCode int, body []byte, err error) {
	if err := s.checkAccountOwnership(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.SimulateAccountStatus(ctx, id, status)
}

func (s *SimulationService) SimulateCardStatus(ctx context.Context, publicKey, id, status string) (statusCode int, body []byte, err error) {
	if err := s.checkCardOwnership(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.SimulateCardStatus(ctx, id, status)
}

func (s *SimulationService) SimulateAuthorization(ctx context.Context, publicKey string, req reap.SimulateCardTransactionRequest) (statusCode int, body []byte, err error) {
	if err := s.checkCardOwnership(ctx, publicKey, req.CardID); err != nil {
		return 0, nil, err
	}
	return s.reap.SimulateAuthorization(ctx, req)
}

func (s *SimulationService) SimulateThreeDSAuthorization(ctx context.Context, publicKey string, req reap.SimulateCardTransactionRequest) (statusCode int, body []byte, err error) {
	if err := s.checkCardOwnership(ctx, publicKey, req.CardID); err != nil {
		return 0, nil, err
	}
	return s.reap.SimulateThreeDSAuthorization(ctx, req)
}

func (s *SimulationService) SimulateDecline(ctx context.Context, publicKey string, req reap.SimulateCardTransactionRequest) (statusCode int, body []byte, err error) {
	if err := s.checkCardOwnership(ctx, publicKey, req.CardID); err != nil {
		return 0, nil, err
	}
	return s.reap.SimulateDecline(ctx, req)
}

func (s *SimulationService) SimulateClearing(ctx context.Context, publicKey string, req reap.SimulateCardTransactionRequest) (statusCode int, body []byte, err error) {
	if err := s.checkCardOwnership(ctx, publicKey, req.CardID); err != nil {
		return 0, nil, err
	}
	return s.reap.SimulateClearing(ctx, req)
}

func (s *SimulationService) SimulateReversal(ctx context.Context, publicKey string, req reap.SimulateCardTransactionRequest) (statusCode int, body []byte, err error) {
	if err := s.checkCardOwnership(ctx, publicKey, req.CardID); err != nil {
		return 0, nil, err
	}
	if err := s.checkTransactionOwnership(ctx, publicKey, req.TransactionID); err != nil {
		return 0, nil, err
	}
	return s.reap.SimulateReversal(ctx, req)
}

func (s *SimulationService) SimulateRefund(ctx context.Context, publicKey string, req reap.SimulateCardTransactionRequest) (statusCode int, body []byte, err error) {
	if err := s.checkCardOwnership(ctx, publicKey, req.CardID); err != nil {
		return 0, nil, err
	}
	return s.reap.SimulateRefund(ctx, req)
}
