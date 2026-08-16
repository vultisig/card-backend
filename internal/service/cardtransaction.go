package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/reap"
)

type CardTransactionService struct {
	reap  *reap.Client
	cards ownershipResolver
}

func NewCardTransactionService(pool *pgxpool.Pool, reapClient *reap.Client) *CardTransactionService {
	return &CardTransactionService{
		reap:  reapClient,
		cards: newCardOwnershipResolver(pool, reapClient),
	}
}

func (s *CardTransactionService) GetCardTransaction(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	status, body, err = s.reap.GetCardTransaction(ctx, id)
	if err != nil || status < 200 || status >= 300 {
		return status, body, err
	}
	var txn struct {
		CardID string `json:"cardId"`
	}
	if err := json.Unmarshal(body, &txn); err != nil || txn.CardID == "" {
		return 0, nil, errors.New("reap: get card transaction response missing cardId")
	}
	if err := s.cards.require(ctx, publicKey, txn.CardID); err != nil {
		return 0, nil, err
	}
	return status, body, nil
}
