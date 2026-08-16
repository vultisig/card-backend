package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/cardownership"
	"github.com/vultisig/card-backend/internal/reap"
)

type CardService struct {
	pool     *pgxpool.Pool
	reap     *reap.Client
	cards    ownershipResolver
	accounts ownershipResolver
}

func NewCardService(pool *pgxpool.Pool, reapClient *reap.Client) *CardService {
	return &CardService{
		pool:     pool,
		reap:     reapClient,
		cards:    newCardOwnershipResolver(pool, reapClient),
		accounts: newAccountOwnershipResolver(pool, reapClient),
	}
}

// CreateCard creates a REAP card owned by publicKey's REAP user (req.UserID
// is overwritten with it), forwarding idempotencyKey to REAP as-is. On a
// 2xx response it records the created card's ID as owned by publicKey, so
// later per-card actions can be scoped to the creating vault. It returns
// ErrNoReapUser if publicKey has no REAP user ID recorded yet.
func (s *CardService) CreateCard(ctx context.Context, publicKey string, req reap.CreateCardRequest, idempotencyKey string) (status int, body []byte, err error) {
	reapUserID, err := resolveReapUserID(ctx, s.pool, publicKey)
	if err != nil {
		return 0, nil, err
	}
	if err := s.accounts.require(ctx, publicKey, req.AccountID); err != nil {
		return 0, nil, err
	}
	req.UserID = reapUserID

	status, body, err = s.reap.CreateCard(ctx, req, idempotencyKey)
	if err != nil {
		return 0, nil, err
	}
	if status < 200 || status >= 300 {
		return status, body, nil
	}

	// The REAP card exists from here on, so a local bookkeeping failure must
	// not turn into a 5xx: the caller would retry with a fresh idempotency
	// key and create a duplicate card. Log it and pass REAP's response
	// through — the ownership resolver heals the missing row from REAP on
	// the next access.
	var created struct {
		ID     string `json:"id"`
		UserID string `json:"userId"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.ID == "" {
		log.Printf("card: created REAP card but failed to parse id from response: %s", body)
		return status, body, nil
	}
	// Record the owner REAP reports, not the one we asked for. Idempotency
	// keys are caller-supplied and share one project-wide REAP API key, so a
	// replayed key can hand back a card created by another vault — which
	// must be neither recorded as this vault's nor returned to it.
	if created.UserID != "" && created.UserID != reapUserID {
		log.Printf("card: REAP returned card %s owned by user %s, not this vault's user %s (replayed idempotency key?)", created.ID, created.UserID, reapUserID)
		return 0, nil, ErrResourceNotOwned
	}
	if err := cardownership.Record(ctx, s.pool, publicKey, created.ID); err != nil {
		log.Printf("card: created REAP card %s but failed to record cardownership for vault %s: %v", created.ID, publicKey, err)
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

// The remaining card-ID methods require the card to be owned by publicKey
// before delegating to reap.Client.

func (s *CardService) GetCard(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.cards.require(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.GetCard(ctx, id)
}

func (s *CardService) DeleteCard(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.cards.require(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.DeleteCard(ctx, id)
}

func (s *CardService) UpdateCardPin(ctx context.Context, publicKey, id, pin string) (status int, body []byte, err error) {
	if err := s.cards.require(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.UpdateCardPin(ctx, id, pin)
}

func (s *CardService) FreezeCard(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.cards.require(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.FreezeCard(ctx, id)
}

func (s *CardService) UnfreezeCard(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.cards.require(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.UnfreezeCard(ctx, id)
}

func (s *CardService) RevealCardDetails(ctx context.Context, publicKey, id, stylesheetURL string, showCopyPanButton bool) (status int, body []byte, err error) {
	if err := s.cards.require(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.RevealCardDetails(ctx, id, stylesheetURL, showCopyPanButton)
}

func (s *CardService) UpdateCard3DSChallengeMethod(ctx context.Context, publicKey, id, method string) (status int, body []byte, err error) {
	if err := s.cards.require(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.UpdateCard3DSChallengeMethod(ctx, id, method)
}

func (s *CardService) ActivatePhysicalCard(ctx context.Context, publicKey, id, activationCode string) (status int, body []byte, err error) {
	if err := s.cards.require(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.ActivatePhysicalCard(ctx, id, activationCode)
}

func (s *CardService) GetCardActivationCode(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	if err := s.cards.require(ctx, publicKey, id); err != nil {
		return 0, nil, err
	}
	return s.reap.GetCardActivationCode(ctx, id)
}

func (s *CardService) PushProvisionCard(ctx context.Context, publicKey, id, provider, walletAccountID, deviceID, idempotencyKey string) (status int, body []byte, err error) {
	if err := s.cards.require(ctx, publicKey, id); err != nil {
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
	if err := s.cards.require(ctx, publicKey, cardID); err != nil {
		return 0, nil, err
	}
	return s.reap.RespondToCard3DSChallenge(ctx, id, approve)
}

func (s *CardService) GetCard3DSChallenge(ctx context.Context, publicKey, id string) (status int, body []byte, err error) {
	status, body, cardID, err := s.challengeCardID(ctx, id)
	if err != nil || cardID == "" {
		return status, body, err
	}
	if err := s.cards.require(ctx, publicKey, cardID); err != nil {
		return 0, nil, err
	}
	return status, body, nil
}
