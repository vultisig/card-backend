package service

import (
	"context"

	"github.com/vultisig/card-backend/internal/reap"
)

// CardTransactionService backs /card-transactions. Transaction IDs aren't
// tracked per-vault, so GetCardTransaction is a thin passthrough to
// reap.Client, same pattern as CardDesignService.
type CardTransactionService struct {
	reap *reap.Client
}

func NewCardTransactionService(reapClient *reap.Client) *CardTransactionService {
	return &CardTransactionService{reap: reapClient}
}

func (s *CardTransactionService) GetCardTransaction(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.GetCardTransaction(ctx, id)
}
