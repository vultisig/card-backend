package service

import (
	"context"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/reap"
)

type CardService struct {
	pool *pgxpool.Pool
	reap *reap.Client
}

func NewCardService(pool *pgxpool.Pool, reapClient *reap.Client) *CardService {
	return &CardService{pool: pool, reap: reapClient}
}

// CreateCard creates a REAP card owned by publicKey's REAP user (req.UserID
// is overwritten with it), forwarding idempotencyKey to REAP as-is. It
// returns ErrNoReapUser if publicKey has no REAP user ID recorded yet.
func (s *CardService) CreateCard(ctx context.Context, publicKey string, req reap.CreateCardRequest, idempotencyKey string) (status int, body []byte, err error) {
	reapUserID, err := resolveReapUserID(ctx, s.pool, publicKey)
	if err != nil {
		return 0, nil, err
	}
	req.UserID = reapUserID
	return s.reap.CreateCard(ctx, req, idempotencyKey)
}

// ListCards lists the REAP cards owned by publicKey's REAP user (query's
// userId is overwritten with it); any other filters in query are forwarded
// as-is. It returns ErrNoReapUser if publicKey has no REAP user ID recorded
// yet.
func (s *CardService) ListCards(ctx context.Context, publicKey string, query url.Values) (status int, body []byte, err error) {
	reapUserID, err := resolveReapUserID(ctx, s.pool, publicKey)
	if err != nil {
		return 0, nil, err
	}
	query.Set("userId", reapUserID)
	return s.reap.ListCards(ctx, query)
}

// The remaining methods act on a card or 3DS challenge ID directly and are
// thin passthroughs to reap.Client — card/challenge IDs aren't tracked
// per-vault, so any authenticated vault can act on any ID it's given (same
// as AccountService's ID-based methods).

func (s *CardService) GetCard(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.GetCard(ctx, id)
}

func (s *CardService) DeleteCard(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.DeleteCard(ctx, id)
}

func (s *CardService) UpdateCardPin(ctx context.Context, id, pin string) (status int, body []byte, err error) {
	return s.reap.UpdateCardPin(ctx, id, pin)
}

func (s *CardService) FreezeCard(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.FreezeCard(ctx, id)
}

func (s *CardService) UnfreezeCard(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.UnfreezeCard(ctx, id)
}

func (s *CardService) RevealCardDetails(ctx context.Context, id, stylesheetURL string, showCopyPanButton bool) (status int, body []byte, err error) {
	return s.reap.RevealCardDetails(ctx, id, stylesheetURL, showCopyPanButton)
}

func (s *CardService) UpdateCard3DSChallengeMethod(ctx context.Context, id, method string) (status int, body []byte, err error) {
	return s.reap.UpdateCard3DSChallengeMethod(ctx, id, method)
}

func (s *CardService) ActivatePhysicalCard(ctx context.Context, id, activationCode string) (status int, body []byte, err error) {
	return s.reap.ActivatePhysicalCard(ctx, id, activationCode)
}

func (s *CardService) GetCardActivationCode(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.GetCardActivationCode(ctx, id)
}

func (s *CardService) PushProvisionCard(ctx context.Context, id, provider, walletAccountID, deviceID, idempotencyKey string) (status int, body []byte, err error) {
	return s.reap.PushProvisionCard(ctx, id, provider, walletAccountID, deviceID, idempotencyKey)
}

func (s *CardService) RespondToCard3DSChallenge(ctx context.Context, id string, approve bool) (status int, body []byte, err error) {
	return s.reap.RespondToCard3DSChallenge(ctx, id, approve)
}

func (s *CardService) GetCard3DSChallenge(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.GetCard3DSChallenge(ctx, id)
}
