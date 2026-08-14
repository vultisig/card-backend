package service

import (
	"context"

	"github.com/vultisig/card-backend/internal/reap"
)

// SimulationService backs /simulation, wrapping REAP's sandbox-only test
// helpers for advancing application/company/account/card status and
// simulating card-transaction lifecycle events. REAP itself rejects these
// calls outside a sandbox environment, so no extra gating is added here.
// Every method is a thin passthrough to reap.Client.
type SimulationService struct {
	reap *reap.Client
}

func NewSimulationService(reapClient *reap.Client) *SimulationService {
	return &SimulationService{reap: reapClient}
}

func (s *SimulationService) SimulateUserApplicationStatus(ctx context.Context, userID, status string) (statusCode int, body []byte, err error) {
	return s.reap.SimulateUserApplicationStatus(ctx, userID, status)
}

func (s *SimulationService) SimulateCompanyStatus(ctx context.Context, companyID string, req reap.SimulateCompanyStatusRequest) (statusCode int, body []byte, err error) {
	return s.reap.SimulateCompanyStatus(ctx, companyID, req)
}

func (s *SimulationService) SimulateAccountStatus(ctx context.Context, id, status string) (statusCode int, body []byte, err error) {
	return s.reap.SimulateAccountStatus(ctx, id, status)
}

func (s *SimulationService) SimulateCardStatus(ctx context.Context, id, status string) (statusCode int, body []byte, err error) {
	return s.reap.SimulateCardStatus(ctx, id, status)
}

func (s *SimulationService) SimulateAuthorization(ctx context.Context, req reap.SimulateCardTransactionRequest) (statusCode int, body []byte, err error) {
	return s.reap.SimulateAuthorization(ctx, req)
}

func (s *SimulationService) SimulateThreeDSAuthorization(ctx context.Context, req reap.SimulateCardTransactionRequest) (statusCode int, body []byte, err error) {
	return s.reap.SimulateThreeDSAuthorization(ctx, req)
}

func (s *SimulationService) SimulateDecline(ctx context.Context, req reap.SimulateCardTransactionRequest) (statusCode int, body []byte, err error) {
	return s.reap.SimulateDecline(ctx, req)
}

func (s *SimulationService) SimulateClearing(ctx context.Context, req reap.SimulateCardTransactionRequest) (statusCode int, body []byte, err error) {
	return s.reap.SimulateClearing(ctx, req)
}

func (s *SimulationService) SimulateReversal(ctx context.Context, req reap.SimulateCardTransactionRequest) (statusCode int, body []byte, err error) {
	return s.reap.SimulateReversal(ctx, req)
}

func (s *SimulationService) SimulateRefund(ctx context.Context, req reap.SimulateCardTransactionRequest) (statusCode int, body []byte, err error) {
	return s.reap.SimulateRefund(ctx, req)
}
