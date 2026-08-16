package service

import (
	"context"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/reap"
)

// ActivityService backs GET /activities. REAP's only filters that identify
// a specific card/account are accountId and cardId (no userId/ownerId), so
// ListActivities requires at least one of them and checks it against the
// card/account ownershipResolver before forwarding, so a vault can't list
// every vault's activity by omitting filters.
type ActivityService struct {
	reap     *reap.Client
	cards    ownershipResolver
	accounts ownershipResolver
}

func NewActivityService(pool *pgxpool.Pool, reapClient *reap.Client) *ActivityService {
	return &ActivityService{
		reap:     reapClient,
		cards:    newCardOwnershipResolver(pool, reapClient),
		accounts: newAccountOwnershipResolver(pool, reapClient),
	}
}

// ListActivities returns ErrMissingScopeFilter if query has neither
// accountId nor cardId set, and ErrResourceNotOwned if either is set to an
// ID that isn't publicKey's. Any unsupported userId/ownerId filter the
// caller passed is dropped before forwarding.
func (s *ActivityService) ListActivities(ctx context.Context, publicKey string, query url.Values) (status int, body []byte, err error) {
	query.Del("userId")
	query.Del("ownerId")

	accountID := query.Get("accountId")
	cardID := query.Get("cardId")
	if accountID == "" && cardID == "" {
		return 0, nil, ErrMissingScopeFilter
	}
	// Collapse any repeated accountId/cardId values to the single one just
	// checked, so a caller can't sneak an extra value past the ownership
	// check via a second occurrence of the query param.
	if cardID != "" {
		query.Set("cardId", cardID)
	}
	if accountID != "" {
		query.Set("accountId", accountID)
	}

	if cardID != "" {
		if err := s.cards.require(ctx, publicKey, cardID); err != nil {
			return 0, nil, err
		}
	}

	if accountID != "" {
		if err := s.accounts.require(ctx, publicKey, accountID); err != nil {
			return 0, nil, err
		}
	}

	return s.reap.ListActivities(ctx, query)
}
