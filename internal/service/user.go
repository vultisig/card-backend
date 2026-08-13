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
func (s *UserService) CreateUser(ctx context.Context, publicKey string, req reap.CreateUserRequest) (status int, body []byte, err error) {
	existing, err := reapmapping.GetReapUserID(ctx, s.pool, publicKey)
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
	if _, err := reapmapping.SetReapUserID(ctx, s.pool, publicKey, created.ID); err != nil {
		return 0, nil, err
	}
	return status, body, nil
}

// GetUser fetches the REAP user for publicKey. It returns ErrNoReapUser if
// publicKey has no REAP user ID recorded yet.
func (s *UserService) GetUser(ctx context.Context, publicKey string) (status int, body []byte, err error) {
	reapUserID, err := reapmapping.GetReapUserID(ctx, s.pool, publicKey)
	if err != nil {
		return 0, nil, err
	}
	if reapUserID == "" {
		return 0, nil, ErrNoReapUser
	}
	return s.reap.GetUser(ctx, reapUserID)
}
