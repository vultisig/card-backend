package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/cardownership"
	"github.com/vultisig/card-backend/internal/reap"
)

type CardTransactionService struct {
	pool *pgxpool.Pool
	reap *reap.Client
}

func NewCardTransactionService(pool *pgxpool.Pool, reapClient *reap.Client) *CardTransactionService {
	return &CardTransactionService{pool: pool, reap: reapClient}
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
	owned, err := cardownership.IsOwner(ctx, s.pool, publicKey, txn.CardID)
	if err != nil {
		return 0, nil, err
	}
	if !owned {
		return 0, nil, ErrResourceNotOwned
	}
	return status, body, nil
}
