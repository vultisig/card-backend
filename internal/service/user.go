package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/vultisig/card-backend/internal/reap"
	"github.com/vultisig/card-backend/internal/reapmapping"
)

var (
	ErrReapUserExists = errors.New("reap user already exists")
	ErrNoReapUser     = errors.New("no reap user for this vault")
	// ErrResourceNotOwned is returned when publicKey's vault requests an
	// action on a REAP resource ID (card, fraud alert, activity filter)
	// that's recorded as owned by a different vault.
	ErrResourceNotOwned = errors.New("resource not owned by this vault")
	// ErrMissingScopeFilter is returned when a list endpoint requires an
	// ownership-checkable filter (e.g. accountId/cardId) and the caller
	// supplied none, since forwarding the query unfiltered would return
	// every vault's resources.
	ErrMissingScopeFilter     = errors.New("a scoping filter is required")
	ErrReapUserCreateInFlight = errors.New("reap user creation already in flight")
)

const (
	// Well clear of the REAP client's 10s timeout, so only a dead request's claim is taken over.
	reapUserCreateStale = time.Minute
	releaseClaimTimeout = 5 * time.Second
)

type UserService struct {
	db   reapmapping.Querier
	reap *reap.Client
}

func NewUserService(db reapmapping.Querier, reapClient *reap.Client) *UserService {
	return &UserService{db: db, reap: reapClient}
}

// CreateUser creates a REAP user for publicKey and records its REAP user ID
// on publicKey's VultisigReapMapping. ErrReapUserExists and
// ErrReapUserCreateInFlight both return without reaching REAP; a non-2xx REAP
// response is passed back unchanged.
//
// Concurrent creates are excluded by a claim on the mapping row rather than a
// lock held across the call, so the round trip holds no pool connection.
func (s *UserService) CreateUser(ctx context.Context, publicKey string, req reap.CreateUserRequest) (status int, body []byte, err error) {
	claimed, err := reapmapping.ClaimReapUserCreate(ctx, s.db, publicKey, reapUserCreateStale)
	if err != nil {
		return 0, nil, err
	}
	if !claimed {
		existing, err := reapmapping.GetReapUserID(ctx, s.db, publicKey)
		if err != nil {
			return 0, nil, err
		}
		if existing != "" {
			return 0, nil, ErrReapUserExists
		}
		return 0, nil, ErrReapUserCreateInFlight
	}

	status, body, err = s.reap.CreateUser(ctx, req)
	if err != nil {
		s.releaseCreateClaim(ctx, publicKey)
		return 0, nil, err
	}
	if status < 200 || status >= 300 {
		s.releaseCreateClaim(ctx, publicKey)
		return status, body, nil
	}

	// No release past this point: a 2xx means REAP has created the user, so a
	// retry would make a second one -- including when we can't read the id back
	// out of the response and have nothing to record.
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.ID == "" {
		return 0, nil, errors.New("reap: create user response missing id")
	}

	ok, err := reapmapping.SetReapUserID(ctx, s.db, publicKey, created.ID)
	if err != nil {
		return 0, nil, err
	}
	if !ok {
		// The claim went stale and a second create won, leaving created.ID orphaned in REAP.
		log.Printf("createUser: reap user %s created for %s but another ID was already recorded", created.ID, publicKey)
	}
	return status, body, nil
}

// Its own context: the request's is already cancelled when the client disconnected mid-call.
func (s *UserService) releaseCreateClaim(ctx context.Context, publicKey string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseClaimTimeout)
	defer cancel()
	if err := reapmapping.ReleaseReapUserCreate(ctx, s.db, publicKey); err != nil {
		log.Printf("createUser: release create claim for %s: %v", publicKey, err)
	}
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
	return resolveReapUserID(ctx, s.db, publicKey)
}

// resolveReapUserID looks up publicKey's REAP user ID, returning
// ErrNoReapUser if it has none recorded yet. Shared by UserService and
// AccountService.
func resolveReapUserID(ctx context.Context, db reapmapping.Querier, publicKey string) (string, error) {
	reapUserID, err := reapmapping.GetReapUserID(ctx, db, publicKey)
	if err != nil {
		return "", err
	}
	if reapUserID == "" {
		return "", ErrNoReapUser
	}
	return reapUserID, nil
}
