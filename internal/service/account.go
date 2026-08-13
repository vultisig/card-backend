package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/reap"
)

type AccountService struct {
	pool *pgxpool.Pool
	reap *reap.Client
}

func NewAccountService(pool *pgxpool.Pool, reapClient *reap.Client) *AccountService {
	return &AccountService{pool: pool, reap: reapClient}
}

// CreateAccount creates a REAP account owned by publicKey's REAP user,
// forwarding idempotencyKey to REAP as-is. It returns ErrNoReapUser if
// publicKey has no REAP user ID recorded yet.
func (s *AccountService) CreateAccount(ctx context.Context, publicKey string, signers *reap.Signers, idempotencyKey string) (status int, body []byte, err error) {
	reapUserID, err := resolveReapUserID(ctx, s.pool, publicKey)
	if err != nil {
		return 0, nil, err
	}
	return s.reap.CreateAccount(ctx, reap.CreateAccountRequest{OwnerID: reapUserID, Signers: signers}, idempotencyKey)
}

func (s *AccountService) GenerateSignerMessage(ctx context.Context) (status int, body []byte, err error) {
	return s.reap.GenerateSignerMessage(ctx)
}

func (s *AccountService) GetAccount(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.GetAccount(ctx, id)
}

func (s *AccountService) GetAccountBalance(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.GetAccountBalance(ctx, id)
}

func (s *AccountService) GetAccountAssets(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.GetAccountAssets(ctx, id)
}

// ListAccounts lists the REAP accounts owned by publicKey's REAP user. It
// returns ErrNoReapUser if publicKey has no REAP user ID recorded yet.
func (s *AccountService) ListAccounts(ctx context.Context, publicKey string, limit int, cursor string) (status int, body []byte, err error) {
	reapUserID, err := resolveReapUserID(ctx, s.pool, publicKey)
	if err != nil {
		return 0, nil, err
	}
	return s.reap.ListAccounts(ctx, reapUserID, limit, cursor)
}
