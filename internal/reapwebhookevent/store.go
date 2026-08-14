// Package reapwebhookevent persists received REAP webhook deliveries
// (models.ReapWebhookEvent), keyed by REAP's event id so redelivered events
// (REAP retries on non-2xx/timeout, and may deliver more than once even on
// success) are recorded only once.
package reapwebhookevent

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Record stores eventID/eventType/payload. It's a no-op if eventID is
// already recorded (a redelivery of an event already processed).
func Record(ctx context.Context, db Querier, eventID, eventType string, payload []byte) error {
	_, err := db.Exec(ctx, `
		INSERT INTO vultisig_reap_webhook_events (event_id, event_type, payload)
		VALUES ($1, $2, $3)
		ON CONFLICT (event_id) DO NOTHING
	`, eventID, eventType, payload)
	return err
}
