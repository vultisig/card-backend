package service

import (
	"context"
	"encoding/json"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/accountownership"
	"github.com/vultisig/card-backend/internal/reap"
)

type AccountService struct {
	pool     *pgxpool.Pool
	reap     *reap.Client
	accounts ownershipResolver
}

func NewAccountService(pool *pgxpool.Pool, reapClient *reap.Client) *AccountService {
	return &AccountService{
		pool:     pool,
		reap:     reapClient,
		accounts: newAccountOwnershipResolver(pool, reapClient),
	}
}

// CreateAccount creates a REAP account owned by publicKey's REAP user,
// forwarding idempotencyKey to REAP as-is, and records the created
// account's ID as owned by publicKey. It returns ErrNoReapUser if
// publicKey has no REAP user ID recorded yet.
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

	// The REAP account exists from here on, so a local bookkeeping failure
	// must not turn into a 5xx: the caller would retry with a fresh
	// idempotency key and create a duplicate account. Log it and pass
	// REAP's response through — the ownership resolver heals the missing
	// row from REAP on the next access.
	var created struct {
		ID      string `json:"id"`
		OwnerID string `json:"ownerId"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.ID == "" {
		log.Printf("account: created REAP account but failed to parse id from response: %s", body)
		return status, body, nil
	}
	// Record the owner REAP reports, not the one we asked for — see the same
	// check in CardService.CreateCard.
	if created.OwnerID != "" && created.OwnerID != reapUserID {
		log.Printf("account: REAP returned account %s owned by user %s, not this vault's user %s (replayed idempotency key?)", created.ID, created.OwnerID, reapUserID)
		return 0, nil, ErrResourceNotOwned
	}
	if err := accountownership.Record(ctx, s.pool, publicKey, created.ID); err != nil {
		log.Printf("account: created REAP account %s but failed to record ownership for vault %s: %v", created.ID, publicKey, err)
	}
	return status, body, nil
}

func (s *AccountService) GenerateSignerMessage(ctx context.Context) (status int, body []byte, err error) {
	return s.reap.GenerateSignerMessage(ctx)
}

func (s *AccountService) GetAccount(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.accounts.require(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.GetAccount(ctx, id)
}

func (s *AccountService) GetAccountBalance(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.accounts.require(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.GetAccountBalance(ctx, id)
}

func (s *AccountService) GetAccountAssets(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.accounts.require(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
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
