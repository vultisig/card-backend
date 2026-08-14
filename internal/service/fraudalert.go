package service

import (
	"context"
	"net/url"

	"github.com/vultisig/card-backend/internal/reap"
)

// FraudAlertService backs /fraud-alerts. Card/transaction IDs aren't
// tracked per-vault, so every method is a thin passthrough to reap.Client,
// same pattern as CardShipmentService.
type FraudAlertService struct {
	reap *reap.Client
}

func NewFraudAlertService(reapClient *reap.Client) *FraudAlertService {
	return &FraudAlertService{reap: reapClient}
}

func (s *FraudAlertService) ListFraudAlerts(ctx context.Context, query url.Values) (status int, body []byte, err error) {
	return s.reap.ListFraudAlerts(ctx, query)
}

func (s *FraudAlertService) ReportFraud(ctx context.Context, req reap.ReportFraudRequest, idempotencyKey string) (status int, body []byte, err error) {
	return s.reap.ReportFraud(ctx, req, idempotencyKey)
}

func (s *FraudAlertService) GetFraudAlert(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.GetFraudAlert(ctx, id)
}

func (s *FraudAlertService) RespondToFraudAlert(ctx context.Context, id string, req reap.RespondToFraudAlertRequest, idempotencyKey string) (status int, body []byte, err error) {
	return s.reap.RespondToFraudAlert(ctx, id, req, idempotencyKey)
}
