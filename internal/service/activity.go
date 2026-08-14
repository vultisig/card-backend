package service

import (
	"context"
	"net/url"

	"github.com/vultisig/card-backend/internal/reap"
)

// ActivityService backs /activities. Account/card IDs aren't tracked
// per-vault, so ListActivities is a thin passthrough to reap.Client, same
// pattern as CardShipmentService.
type ActivityService struct {
	reap *reap.Client
}

func NewActivityService(reapClient *reap.Client) *ActivityService {
	return &ActivityService{reap: reapClient}
}

func (s *ActivityService) ListActivities(ctx context.Context, query url.Values) (status int, body []byte, err error) {
	return s.reap.ListActivities(ctx, query)
}
