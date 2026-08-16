package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/reap"
)

type CardShipmentService struct {
	reap  *reap.Client
	cards ownershipResolver
}

func NewCardShipmentService(pool *pgxpool.Pool, reapClient *reap.Client) *CardShipmentService {
	return &CardShipmentService{
		reap:  reapClient,
		cards: newCardOwnershipResolver(pool, reapClient),
	}
}

func (s *CardShipmentService) checkShipmentMembers(ctx context.Context, publicKey string, members []reap.CardShipmentMember) error {
	if len(members) == 0 {
		return ErrMissingScopeFilter
	}
	for _, member := range members {
		if member.CardID == "" {
			return ErrMissingScopeFilter
		}
		if err := s.cards.require(ctx, publicKey, member.CardID); err != nil {
			return err
		}
	}
	return nil
}

func shipmentCardIDs(body []byte) ([]string, error) {
	var shipment struct {
		Cards []struct {
			CardID string `json:"cardId"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(body, &shipment); err != nil {
		return nil, err
	}
	cardIDs := make([]string, 0, len(shipment.Cards))
	for _, card := range shipment.Cards {
		if card.CardID != "" {
			cardIDs = append(cardIDs, card.CardID)
		}
	}
	if len(cardIDs) == 0 {
		return nil, errors.New("reap: get card shipment response missing cards.cardId")
	}
	return cardIDs, nil
}

func (s *CardShipmentService) fetchOwnedShipment(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	status, body, err = s.reap.GetCardShipment(ctx, id)
	if err != nil || status < 200 || status >= 300 {
		return status, body, err
	}
	cardIDs, err := shipmentCardIDs(body)
	if err != nil {
		return 0, nil, err
	}
	for _, cardID := range cardIDs {
		if err := s.cards.require(ctx, publicKey, cardID); err != nil {
			return 0, nil, err
		}
	}
	return status, body, nil
}

func (s *CardShipmentService) CreateCardShipment(ctx context.Context, publicKey string, req reap.CreateCardShipmentRequest) (status int, body []byte, err error) {
	if err := s.checkShipmentMembers(ctx, publicKey, req.Cards); err != nil {
		return 0, nil, err
	}
	return s.reap.CreateCardShipment(ctx, req)
}

func (s *CardShipmentService) ListCardShipments(ctx context.Context, publicKey string, query url.Values) (status int, body []byte, err error) {
	cardID := query.Get("cardId")
	if cardID == "" {
		return 0, nil, ErrMissingScopeFilter
	}
	if err := s.cards.require(ctx, publicKey, cardID); err != nil {
		return 0, nil, err
	}
	query.Set("cardId", cardID)
	return s.reap.ListCardShipments(ctx, query)
}

func (s *CardShipmentService) GetCardShipment(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	return s.fetchOwnedShipment(ctx, publicKey, id)
}

func (s *CardShipmentService) DeleteCardShipment(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if status, body, err := s.fetchOwnedShipment(ctx, publicKey, id); err != nil || status < 200 || status >= 300 {
		return status, body, err
	}
	return s.reap.DeleteCardShipment(ctx, id)
}

func (s *CardShipmentService) UpdateCardShipment(ctx context.Context, publicKey, id string, req reap.UpdateCardShipmentRequest) (status int, body []byte, err error) {
	if status, body, err := s.fetchOwnedShipment(ctx, publicKey, id); err != nil || status < 200 || status >= 300 {
		return status, body, err
	}
	return s.reap.UpdateCardShipment(ctx, id, req)
}

func (s *CardShipmentService) AddCardToShipment(ctx context.Context, publicKey, id string, member reap.CardShipmentMember) (status int, body []byte, err error) {
	if status, body, err := s.fetchOwnedShipment(ctx, publicKey, id); err != nil || status < 200 || status >= 300 {
		return status, body, err
	}
	if member.CardID == "" {
		return 0, nil, ErrMissingScopeFilter
	}
	if err := s.cards.require(ctx, publicKey, member.CardID); err != nil {
		return 0, nil, err
	}
	return s.reap.AddCardToShipment(ctx, id, member)
}

func (s *CardShipmentService) RemoveCardFromShipment(ctx context.Context, publicKey, id, memberID string) (status int, body []byte, err error) {
	if status, body, err := s.fetchOwnedShipment(ctx, publicKey, id); err != nil || status < 200 || status >= 300 {
		return status, body, err
	}
	return s.reap.RemoveCardFromShipment(ctx, id, memberID)
}

func (s *CardShipmentService) SubmitCardShipment(ctx context.Context, publicKey, id, idempotencyKey string) (status int, body []byte, err error) {
	if status, body, err := s.fetchOwnedShipment(ctx, publicKey, id); err != nil || status < 200 || status >= 300 {
		return status, body, err
	}
	return s.reap.SubmitCardShipment(ctx, id, idempotencyKey)
}
