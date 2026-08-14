package service

import (
	"context"
	"errors"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/reap"
	"github.com/vultisig/card-backend/internal/resourceownership"
)

// ActivityService backs GET /activities. REAP's only filters that identify
// a specific card/account are accountId and cardId (no userId/ownerId), so
// ListActivities requires at least one of them and checks it against
// resourceownership before forwarding, so a vault can't list every vault's
// activity by omitting filters.
type ActivityService struct {
	pool *pgxpool.Pool
	reap *reap.Client
}

func NewActivityService(pool *pgxpool.Pool, reapClient *reap.Client) *ActivityService {
	return &ActivityService{pool: pool, reap: reapClient}
}

// ListActivities returns ErrMissingScopeFilter if query has neither
// accountId nor cardId set, and ErrResourceNotOwned if either is set to an
// ID that isn't recorded as owned by publicKey's REAP user (including if
// publicKey has no REAP user at all). Any unsupported userId/ownerId filter
// the caller passed is dropped before forwarding.
func (s *ActivityService) ListActivities(ctx context.Context, publicKey string, query url.Values) (status int, body []byte, err error) {
	query.Del("userId")
	query.Del("ownerId")

	accountID := query.Get("accountId")
	cardID := query.Get("cardId")
	if accountID == "" && cardID == "" {
		return 0, nil, ErrMissingScopeFilter
	}

	reapUserID, err := resolveReapUserID(ctx, s.pool, publicKey)
	if errors.Is(err, ErrNoReapUser) {
		return 0, nil, ErrResourceNotOwned
	}
	if err != nil {
		return 0, nil, err
	}

	if cardID != "" {
		owned, err := resourceownership.IsOwner(ctx, s.pool, resourceownership.KindCard, cardID, reapUserID)
		if err != nil {
			return 0, nil, err
		}
		if !owned {
			return 0, nil, ErrResourceNotOwned
		}
	}

	if accountID != "" {
		owned, err := resourceownership.IsOwner(ctx, s.pool, resourceownership.KindAccount, accountID, reapUserID)
		if err != nil {
			return 0, nil, err
		}
		if !owned {
			return 0, nil, ErrResourceNotOwned
		}
	}

	return s.reap.ListActivities(ctx, query)
}
