// Package webhook is the single ingestion point for Reap webhook events:
// signature verification, replay/deduplication by event id, and
// out-of-order-safe dispatch to consuming packages.
//
// Handlers must be idempotent: deliveries repeat and are not ordered.
package webhook
