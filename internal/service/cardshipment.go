package service

import (
	"context"
	"net/url"

	"github.com/vultisig/card-backend/internal/reap"
)

// CardShipmentService backs /card-shipments. REAP shipments aren't tied to
// a vault's REAP user (only to cards), so every method is a thin
// passthrough to reap.Client — same pattern as AccountService's ID-based
// methods.
type CardShipmentService struct {
	reap *reap.Client
}

func NewCardShipmentService(reapClient *reap.Client) *CardShipmentService {
	return &CardShipmentService{reap: reapClient}
}

func (s *CardShipmentService) CreateCardShipment(ctx context.Context, req reap.CreateCardShipmentRequest) (status int, body []byte, err error) {
	return s.reap.CreateCardShipment(ctx, req)
}

func (s *CardShipmentService) ListCardShipments(ctx context.Context, query url.Values) (status int, body []byte, err error) {
	return s.reap.ListCardShipments(ctx, query)
}

func (s *CardShipmentService) GetCardShipment(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.GetCardShipment(ctx, id)
}

func (s *CardShipmentService) DeleteCardShipment(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.DeleteCardShipment(ctx, id)
}

func (s *CardShipmentService) UpdateCardShipment(ctx context.Context, id string, req reap.UpdateCardShipmentRequest) (status int, body []byte, err error) {
	return s.reap.UpdateCardShipment(ctx, id, req)
}

func (s *CardShipmentService) AddCardToShipment(ctx context.Context, id string, member reap.CardShipmentMember) (status int, body []byte, err error) {
	return s.reap.AddCardToShipment(ctx, id, member)
}

func (s *CardShipmentService) RemoveCardFromShipment(ctx context.Context, id, memberID string) (status int, body []byte, err error) {
	return s.reap.RemoveCardFromShipment(ctx, id, memberID)
}

func (s *CardShipmentService) SubmitCardShipment(ctx context.Context, id, idempotencyKey string) (status int, body []byte, err error) {
	return s.reap.SubmitCardShipment(ctx, id, idempotencyKey)
}
