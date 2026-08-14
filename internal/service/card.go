package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/accountownership"
	"github.com/vultisig/card-backend/internal/cardownership"
	"github.com/vultisig/card-backend/internal/reap"
	"github.com/vultisig/card-backend/internal/usercardownership"
)

type CardService struct {
	pool *pgxpool.Pool
	reap *reap.Client
}

func NewCardService(pool *pgxpool.Pool, reapClient *reap.Client) *CardService {
	return &CardService{pool: pool, reap: reapClient}
}

// CreateCard creates a REAP card owned by publicKey's REAP user (req.UserID
// is overwritten with it), forwarding idempotencyKey to REAP as-is. On a
// 2xx response it records the created card's ID as owned by publicKey (via
// cardownership, used to scope later per-card actions to the creating
// vault) and by that REAP user (via usercardownership, mirroring REAP's own
// userId-based ownership model). It returns ErrNoReapUser if publicKey has
// no REAP user ID recorded yet.
func (s *CardService) CreateCard(ctx context.Context, publicKey string, req reap.CreateCardRequest, idempotencyKey string) (status int, body []byte, err error) {
	reapUserID, err := resolveReapUserID(ctx, s.pool, publicKey)
	if err != nil {
		return 0, nil, err
	}
	owned, err := accountownership.IsOwner(ctx, s.pool, publicKey, req.AccountID)
	if err != nil {
		return 0, nil, err
	}
	if !owned {
		return 0, nil, ErrResourceNotOwned
	}
	req.UserID = reapUserID

	status, body, err = s.reap.CreateCard(ctx, req, idempotencyKey)
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
		// The REAP card already exists at this point but its ID couldn't be
		// parsed out, so it can't be recorded as owned by anyone; log the
		// raw response so it's recoverable.
		log.Printf("card: created REAP card but failed to parse id from response: %s", body)
		return 0, nil, errors.New("reap: create card response missing id")
	}
	if err := cardownership.Record(ctx, s.pool, publicKey, created.ID); err != nil {
		log.Printf("card: created REAP card %s but failed to record cardownership: %v", created.ID, err)
		return 0, nil, err
	}
	if err := usercardownership.Record(ctx, s.pool, reapUserID, created.ID); err != nil {
		log.Printf("card: created REAP card %s but failed to record usercardownership: %v", created.ID, err)
		return 0, nil, err
	}
	return status, body, nil
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

// checkOwnership returns ErrResourceNotOwned if cardID isn't recorded as
// owned by publicKey (including if it has no ownership record at all).
func (s *CardService) checkOwnership(ctx context.Context, publicKey, cardID string) error {
	owned, err := cardownership.IsOwner(ctx, s.pool, publicKey, cardID)
	if err != nil {
		return err
	}
	if !owned {
		return ErrResourceNotOwned
	}
	return nil
}

// The remaining card-ID methods verify the card is owned by publicKey (via
// checkOwnership) before delegating to reap.Client.

func (s *CardService) GetCard(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.checkOwnership(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.GetCard(ctx, id)
}

func (s *CardService) DeleteCard(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.checkOwnership(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.DeleteCard(ctx, id)
}

func (s *CardService) UpdateCardPin(ctx context.Context, publicKey, id, pin string) (status int, body []byte, err error) {
	if err := s.checkOwnership(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.UpdateCardPin(ctx, id, pin)
}

func (s *CardService) FreezeCard(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.checkOwnership(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.FreezeCard(ctx, id)
}

func (s *CardService) UnfreezeCard(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.checkOwnership(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.UnfreezeCard(ctx, id)
}

func (s *CardService) RevealCardDetails(ctx context.Context, publicKey, id, stylesheetURL string, showCopyPanButton bool) (status int, body []byte, err error) {
	if err := s.checkOwnership(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.RevealCardDetails(ctx, id, stylesheetURL, showCopyPanButton)
}

func (s *CardService) UpdateCard3DSChallengeMethod(ctx context.Context, publicKey, id, method string) (status int, body []byte, err error) {
	if err := s.checkOwnership(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.UpdateCard3DSChallengeMethod(ctx, id, method)
}

func (s *CardService) ActivatePhysicalCard(ctx context.Context, publicKey, id, activationCode string) (status int, body []byte, err error) {
	if err := s.checkOwnership(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.ActivatePhysicalCard(ctx, id, activationCode)
}

func (s *CardService) GetCardActivationCode(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.checkOwnership(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.GetCardActivationCode(ctx, id)
}

func (s *CardService) PushProvisionCard(ctx context.Context, publicKey, id, provider, walletAccountID, deviceID, idempotencyKey string) (status int, body []byte, err error) {
	if err := s.checkOwnership(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.PushProvisionCard(ctx, id, provider, walletAccountID, deviceID, idempotencyKey)
}

func (s *CardService) challengeCardID(ctx context.Context, id string) (status int, body []byte, cardID string, err error) {
	status, body, err = s.reap.GetCard3DSChallenge(ctx, id)
	if err != nil {
		return 0, nil, "", err
	}
	if status < 200 || status >= 300 {
		return status, body, "", nil
	}
	var challenge struct {
		CardID string `json:"cardId"`
	}
	if err := json.Unmarshal(body, &challenge); err != nil || challenge.CardID == "" {
		return 0, nil, "", errors.New("reap: get card 3ds challenge response missing cardId")
	}
	return status, body, challenge.CardID, nil
}

func (s *CardService) RespondToCard3DSChallenge(ctx context.Context, publicKey, id string, approve bool) (status int, body []byte, err error) {
	status, body, cardID, err := s.challengeCardID(ctx, id)
	if err != nil || cardID == "" {
		return status, body, err
	}
	if err := s.checkOwnership(ctx, publicKey, cardID); err != nil {
		return 0, nil, err
	}
	return s.reap.RespondToCard3DSChallenge(ctx, id, approve)
}

func (s *CardService) GetCard3DSChallenge(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	status, body, cardID, err := s.challengeCardID(ctx, id)
	if err != nil || cardID == "" {
		return status, body, err
	}
	if err := s.checkOwnership(ctx, publicKey, cardID); err != nil {
		return 0, nil, err
	}
	return status, body, nil
}
