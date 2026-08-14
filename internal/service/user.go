package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/reap"
	"github.com/vultisig/card-backend/internal/reapmapping"
)

var (
	ErrReapUserExists = errors.New("reap user already exists")
	ErrNoReapUser     = errors.New("no reap user for this vault")
)

type UserService struct {
	pool *pgxpool.Pool
	reap *reap.Client
}

func NewUserService(pool *pgxpool.Pool, reapClient *reap.Client) *UserService {
	return &UserService{pool: pool, reap: reapClient}
}

// CreateUser creates a REAP user for publicKey and records its REAP user ID
// on publicKey's VultisigReapMapping. It returns ErrReapUserExists without
// calling REAP if publicKey already has a REAP user ID.
//
// On a non-2xx REAP response, it returns REAP's status/body unchanged (no
// error) so the caller can pass REAP's error straight through.
//
// The existence check, REAP call, and mapping write all happen while
// holding a Postgres advisory lock on publicKey, so two concurrent requests
// for the same vault can't both pass the check and each create a REAP user.
func (s *UserService) CreateUser(ctx context.Context, publicKey string, req reap.CreateUserRequest) (status int, body []byte, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, publicKey); err != nil {
		return 0, nil, err
	}

	existing, err := reapmapping.GetReapUserID(ctx, tx, publicKey)
	if err != nil {
		return 0, nil, err
	}
	if existing != "" {
		return 0, nil, ErrReapUserExists
	}

	status, body, err = s.reap.CreateUser(ctx, req)
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
		return 0, nil, errors.New("reap: create user response missing id")
	}
	if _, err := reapmapping.SetReapUserID(ctx, tx, publicKey, created.ID); err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, err
	}
	return status, body, nil
}

// GetUser fetches the REAP user for publicKey. It returns ErrNoReapUser if
// publicKey has no REAP user ID recorded yet.
func (s *UserService) GetUser(ctx context.Context, publicKey string) (status int, body []byte, err error) {
	reapUserID, err := s.reapUserID(ctx, publicKey)
	if err != nil {
		return 0, nil, err
	}
	return s.reap.GetUser(ctx, reapUserID)
}

// UpdateEmail updates the email of the REAP user for publicKey. It returns
// ErrNoReapUser if publicKey has no REAP user ID recorded yet.
func (s *UserService) UpdateEmail(ctx context.Context, publicKey, email string) (status int, body []byte, err error) {
	reapUserID, err := s.reapUserID(ctx, publicKey)
	if err != nil {
		return 0, nil, err
	}
	return s.reap.UpdateEmail(ctx, reapUserID, email)
}

// UpdatePhoneNumber updates the phone number of the REAP user for
// publicKey. It returns ErrNoReapUser if publicKey has no REAP user ID
// recorded yet.
func (s *UserService) UpdatePhoneNumber(ctx context.Context, publicKey, phoneNumber string) (status int, body []byte, err error) {
	reapUserID, err := s.reapUserID(ctx, publicKey)
	if err != nil {
		return 0, nil, err
	}
	return s.reap.UpdatePhoneNumber(ctx, reapUserID, phoneNumber)
}

// AdvanceUserApplication advances the REAP KYC application for publicKey's
// REAP user, forwarding body and idempotencyKey to REAP as-is. It returns
// ErrNoReapUser if publicKey has no REAP user ID recorded yet.
func (s *UserService) AdvanceUserApplication(ctx context.Context, publicKey string, body []byte, idempotencyKey string) (status int, respBody []byte, err error) {
	reapUserID, err := s.reapUserID(ctx, publicKey)
	if err != nil {
		return 0, nil, err
	}
	return s.reap.AdvanceUserApplication(ctx, reapUserID, body, idempotencyKey)
}

func (s *UserService) reapUserID(ctx context.Context, publicKey string) (string, error) {
	return resolveReapUserID(ctx, s.pool, publicKey)
}

// resolveReapUserID looks up publicKey's REAP user ID, returning
// ErrNoReapUser if it has none recorded yet. Shared by UserService and
// AccountService.
func resolveReapUserID(ctx context.Context, pool *pgxpool.Pool, publicKey string) (string, error) {
	reapUserID, err := reapmapping.GetReapUserID(ctx, pool, publicKey)
	if err != nil {
		return "", err
	}
	if reapUserID == "" {
		return "", ErrNoReapUser
	}
	return reapUserID, nil
}
