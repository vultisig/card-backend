package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/reap"
	"github.com/vultisig/card-backend/internal/resourceownership"
)

type AccountService struct {
	pool *pgxpool.Pool
	reap *reap.Client
}

func NewAccountService(pool *pgxpool.Pool, reapClient *reap.Client) *AccountService {
	return &AccountService{pool: pool, reap: reapClient}
}

// CreateAccount creates a REAP account owned by publicKey's REAP user,
// forwarding idempotencyKey to REAP as-is, and records the created
// account's ID as owned by that REAP user (tracking only — GetAccount/
// GetAccountBalance/GetAccountAssets don't enforce it below, since REAP
// accounts can have co-signers beyond their creator). It returns
// ErrNoReapUser if publicKey has no REAP user ID recorded yet.
func (s *AccountService) CreateAccount(ctx context.Context, publicKey string, signers *reap.Signers, idempotencyKey string) (status int, body []byte, err error) {
	reapUserID, err := resolveReapUserID(ctx, s.pool, publicKey)
	if err != nil {
		return 0, nil, err
	}

	status, body, err = s.reap.CreateAccount(ctx, reap.CreateAccountRequest{OwnerID: reapUserID, Signers: signers}, idempotencyKey)
	if err != nil {
		return 0, nil, err
	}
	if status < 200 || status >= 300 {
		return status, body, nil
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.ID == "" {
		return 0, nil, errors.New("reap: create account response missing id")
	}
	if err := resourceownership.Record(ctx, s.pool, resourceownership.KindAccount, created.ID, reapUserID); err != nil {
		return 0, nil, err
	}
	return status, body, nil
}

func (s *AccountService) GenerateSignerMessage(ctx context.Context) (status int, body []byte, err error) {
	return s.reap.GenerateSignerMessage(ctx)
}

// GetAccount is a thin, unscoped passthrough — see CreateAccount's comment
// on why account ownership isn't enforced here.
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
