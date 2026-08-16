package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/card-backend/internal/accountownership"
	"github.com/vultisig/card-backend/internal/cardownership"
	"github.com/vultisig/card-backend/internal/reap"
)

// ownershipResolver answers "may this vault act on this REAP resource?",
// treating the local ownership table as a cache over REAP's own owner field
// rather than the source of truth.
//
// The local row can be missing even though the resource exists: a create
// that reached REAP but died before the insert, a resource created outside
// this backend, or a restored database all leave a card/account nobody can
// ever use. So a local miss falls back to REAP and, when REAP agrees the
// caller owns the resource, backfills the row and allows the call.
//
// A local row naming a *different* vault is the one case that isn't healed
// automatically — see require.
//
// Its dependencies are function fields rather than concrete stores so the
// decision logic is unit-testable without a database.
type ownershipResolver struct {
	// resource labels this resolver's kind ("card"/"account") in log lines.
	resource string
	// localOwner returns the vault recorded as owning id.
	localOwner func(ctx context.Context, id string) (owner string, found bool, err error)
	// recordLocal records id as owned by publicKey.
	recordLocal func(ctx context.Context, publicKey, id string) error
	// reapOwner returns the REAP user ID that owns id, or "" if REAP has no
	// such resource. An upstream failure is an error, never "".
	reapOwner func(ctx context.Context, id string) (reapUserID string, err error)
	// callerReapUserID resolves publicKey's REAP user ID.
	callerReapUserID func(ctx context.Context, publicKey string) (string, error)
}

// require returns nil if publicKey may act on id, and ErrResourceNotOwned
// if it may not. The cases, in the order they're checked:
//
//   - local row matches the caller: allowed, without calling REAP.
//   - no local row, REAP says the caller's user owns it: backfilled and
//     allowed. This is the crash-between-REAP-and-insert case.
//   - no local row, REAP disagrees or has no such resource: denied.
//   - local row names another vault: denied, and the row is left alone. If
//     REAP disagrees with that row it's logged as a conflict for alerting.
//     Rewriting the row on REAP's word would let a caller take over another
//     vault's resource whenever our vault -> REAP user mapping is wrong, so
//     this one needs a human.
func (r ownershipResolver) require(ctx context.Context, publicKey, id string) error {
	if id == "" {
		return ErrResourceNotOwned
	}

	owner, found, err := r.localOwner(ctx, id)
	if err != nil {
		return err
	}
	if found && owner == publicKey {
		return nil
	}

	callerUserID, err := r.callerReapUserID(ctx, publicKey)
	if err != nil {
		// A vault with no REAP user can't own anything in REAP either, so
		// there's nothing to heal from — that's a plain denial, not the
		// ErrNoReapUser the create/list endpoints report.
		if errors.Is(err, ErrNoReapUser) {
			return ErrResourceNotOwned
		}
		return err
	}

	reapUserID, err := r.reapOwner(ctx, id)
	if err != nil {
		return err
	}
	if reapUserID == "" || reapUserID != callerUserID {
		return ErrResourceNotOwned
	}

	if found {
		log.Printf("ownership conflict: %s %s recorded to vault %s but REAP reports user %s (caller %s)",
			r.resource, id, owner, reapUserID, publicKey)
		return ErrResourceNotOwned
	}

	if err := r.recordLocal(ctx, publicKey, id); err != nil {
		// REAP already confirmed ownership, so failing to cache it is no
		// reason to deny the call; the next access retries the backfill.
		log.Printf("ownership: %s %s confirmed by REAP but backfill for vault %s failed: %v",
			r.resource, id, publicKey, err)
	}
	return nil
}

// reapResourceFound classifies the REAP GET behind an ownership check.
//
// A 4xx means REAP won't hand us this resource — no such ID, or one the
// caller can't have — which is a denial, not a failure, so it reports false
// with a nil error. The IDs reaching here come straight from callers, so a
// malformed one drawing a 400/422 must not read as an outage.
//
// 429 and 5xx are the opposite: the resource may well be the caller's and
// REAP just can't say right now. Those come back as errors so they surface
// as a 5xx, instead of a REAP outage silently denying every heal attempt
// with "this resource isn't yours".
func reapResourceFound(resource, id string, status int, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	if status >= 200 && status < 300 {
		return true, nil
	}
	if status >= 400 && status < 500 && status != http.StatusTooManyRequests {
		return false, nil
	}
	return false, fmt.Errorf("reap: get %s %s: status %d", resource, id, status)
}

func newCardOwnershipResolver(pool *pgxpool.Pool, client *reap.Client) ownershipResolver {
	return ownershipResolver{
		resource: "card",
		localOwner: func(ctx context.Context, id string) (string, bool, error) {
			return cardownership.Owner(ctx, pool, id)
		},
		recordLocal: func(ctx context.Context, publicKey, id string) error {
			return cardownership.Record(ctx, pool, publicKey, id)
		},
		reapOwner: func(ctx context.Context, id string) (string, error) {
			status, body, err := client.GetCard(ctx, id)
			found, err := reapResourceFound("card", id, status, err)
			if !found || err != nil {
				return "", err
			}
			var card struct {
				UserID string `json:"userId"`
			}
			if err := json.Unmarshal(body, &card); err != nil {
				return "", err
			}
			if card.UserID == "" {
				// A card REAP hands back without a userId is a schema
				// surprise, not a normal denial — every heal would fail
				// silently. Say so once per lookup rather than blaming
				// the caller.
				log.Printf("ownership: REAP returned card %s with no userId; healing cannot work against this response shape", id)
			}
			return card.UserID, nil
		},
		callerReapUserID: func(ctx context.Context, publicKey string) (string, error) {
			return resolveReapUserID(ctx, pool, publicKey)
		},
	}
}

func newAccountOwnershipResolver(pool *pgxpool.Pool, client *reap.Client) ownershipResolver {
	return ownershipResolver{
		resource: "account",
		localOwner: func(ctx context.Context, id string) (string, bool, error) {
			return accountownership.Owner(ctx, pool, id)
		},
		recordLocal: func(ctx context.Context, publicKey, id string) error {
			return accountownership.Record(ctx, pool, publicKey, id)
		},
		reapOwner: func(ctx context.Context, id string) (string, error) {
			status, body, err := client.GetAccount(ctx, id)
			found, err := reapResourceFound("account", id, status, err)
			if !found || err != nil {
				return "", err
			}
			var account struct {
				OwnerID string `json:"ownerId"`
			}
			if err := json.Unmarshal(body, &account); err != nil {
				return "", err
			}
			if account.OwnerID == "" {
				log.Printf("ownership: REAP returned account %s with no ownerId; healing cannot work against this response shape", id)
			}
			return account.OwnerID, nil
		},
		callerReapUserID: func(ctx context.Context, publicKey string) (string, error) {
			return resolveReapUserID(ctx, pool, publicKey)
		},
	}
}
