// Package notification enqueues push-notification requests directly onto
// the Vultisig notification service's Redis-backed asynq queue — the same
// queue its own POST /notify handler enqueues onto (see the notification
// repo's api/server.go and models/tasks.go) — instead of making an HTTP
// round trip for every REAP webhook event. The notification service's
// worker (cmd/worker/main.go) consumes the queue exactly as it would a
// request that came in over HTTP.
package notification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// queueName and taskType mirror models.QUEUE_NAME/TypeNotification in the
// notification service (models/tasks.go) — the queue/task type its worker
// is registered to consume (cmd/worker/main.go).
const (
	queueName = "notification"
	taskType  = "key:notification"
)

type Client struct {
	asynq *asynq.Client
}

// NewClient builds a Client that enqueues onto the Redis instance at
// redisURL (a redis:// or rediss:// URI, as accepted by redis.ParseURL).
// Returns an error if redisURL is empty or doesn't parse — the caller
// decides whether that's fatal.
func NewClient(redisURL string) (*Client, error) {
	if redisURL == "" {
		return nil, errors.New("notification: redis url not configured")
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	return &Client{asynq: asynq.NewClient(asynq.RedisClientOpt{
		Addr:      opt.Addr,
		Username:  opt.Username,
		Password:  opt.Password,
		DB:        opt.DB,
		TLSConfig: opt.TLSConfig,
	})}, nil
}

// Close releases the underlying Redis connection pool.
func (c *Client) Close() error {
	return c.asynq.Close()
}

// Request mirrors models.NotificationRequest's JSON shape in the
// notification service (models/notification.go) — its worker unmarshals
// the task payload directly into that struct.
type Request struct {
	VaultID string `json:"vault_id"`
	// Type must be set to something other than "" (which the notification
	// service defaults to "keysign" and then requires vault_name/
	// qr_code_data/local_party_id for) — "generic" only requires VaultID.
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// Notify enqueues req onto the notification service's queue, matching the
// asynq options its own POST /notify handler uses so a stuck task is
// retried/expired the same way.
func (c *Client) Notify(ctx context.Context, req Request) error {
	if req.Type == "" {
		return errors.New("notification: type is required")
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = c.asynq.EnqueueContext(ctx, asynq.NewTask(taskType, payload),
		asynq.Queue(queueName),
		asynq.MaxRetry(-1),
		asynq.Timeout(time.Minute),
		asynq.Retention(time.Minute),
	)
	return err
}

// VaultID computes the notification service's vault_id for a card-backend
// vault: SHA256(utf8(pubKeyECDSA + hexChainCode)) as lowercase hex, matching
// the client SDKs' computeNotificationVaultId.
func VaultID(pubKeyECDSA, hexChainCode string) string {
	sum := sha256.Sum256([]byte(pubKeyECDSA + hexChainCode))
	return hex.EncodeToString(sum[:])
}
