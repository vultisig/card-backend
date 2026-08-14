package service

import (
	"context"
	"net/url"

	"github.com/vultisig/card-backend/internal/reap"
)

// CardDesignService backs /card-designs. Designs are assigned to the
// project (not per-vault) by REAP out of band, so every method is a thin
// passthrough to reap.Client.
type CardDesignService struct {
	reap *reap.Client
}

func NewCardDesignService(reapClient *reap.Client) *CardDesignService {
	return &CardDesignService{reap: reapClient}
}

func (s *CardDesignService) ListCardDesigns(ctx context.Context, query url.Values) (status int, body []byte, err error) {
	return s.reap.ListCardDesigns(ctx, query)
}

func (s *CardDesignService) GetCardDesign(ctx context.Context, id string) (status int, body []byte, err error) {
	return s.reap.GetCardDesign(ctx, id)
}
