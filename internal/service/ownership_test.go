package service

import (
	"context"
	"errors"
	"testing"
)

// fakeOwnership builds a resolver over in-memory state: local holds the
// ownership rows, remote holds REAP's view (resource ID -> REAP user ID),
// and users maps vault public keys to REAP user IDs.
type fakeOwnership struct {
	local  map[string]string
	remote map[string]string
	users  map[string]string

	localCalls  int
	remoteCalls int
	recordCalls int
	localErr    error
	remoteErr   error
	recordErr   error
}

func (f *fakeOwnership) resolver() ownershipResolver {
	return ownershipResolver{
		resource: "card",
		localOwner: func(_ context.Context, id string) (string, bool, error) {
			f.localCalls++
			if f.localErr != nil {
				return "", false, f.localErr
			}
			owner, found := f.local[id]
			return owner, found, nil
		},
		recordLocal: func(_ context.Context, publicKey, id string) error {
			f.recordCalls++
			if f.recordErr != nil {
				return f.recordErr
			}
			f.local[id] = publicKey
			return nil
		},
		reapOwner: func(_ context.Context, id string) (string, error) {
			f.remoteCalls++
			if f.remoteErr != nil {
				return "", f.remoteErr
			}
			return f.remote[id], nil
		},
		callerReapUserID: func(_ context.Context, publicKey string) (string, error) {
			userID, ok := f.users[publicKey]
			if !ok {
				return "", ErrNoReapUser
			}
			return userID, nil
		},
	}
}

func newFakeOwnership() *fakeOwnership {
	return &fakeOwnership{
		local:  map[string]string{},
		remote: map[string]string{},
		users:  map[string]string{"vault-a": "user-a", "vault-b": "user-b"},
	}
}

// A local row matching the caller is answered from the local table alone —
// the common path must not pay for a REAP round trip.
func TestRequireLocalHit(t *testing.T) {
	f := newFakeOwnership()
	f.local["card-1"] = "vault-a"

	if err := f.resolver().require(context.Background(), "vault-a", "card-1"); err != nil {
		t.Fatalf("require() = %v, want nil", err)
	}
	if f.remoteCalls != 0 {
		t.Fatalf("remote lookups = %d, want 0", f.remoteCalls)
	}
}

// The crash-between-REAP-and-insert case: REAP has the card and says it's
// the caller's, so the missing row is backfilled and the call allowed.
func TestRequireHealsMissingRow(t *testing.T) {
	f := newFakeOwnership()
	f.remote["card-1"] = "user-a"

	if err := f.resolver().require(context.Background(), "vault-a", "card-1"); err != nil {
		t.Fatalf("require() = %v, want nil", err)
	}
	if got := f.local["card-1"]; got != "vault-a" {
		t.Fatalf("backfilled owner = %q, want vault-a", got)
	}
}

// REAP confirmed ownership, so a failed backfill must not deny the call —
// the next access retries it.
func TestRequireAllowsWhenBackfillFails(t *testing.T) {
	f := newFakeOwnership()
	f.remote["card-1"] = "user-a"
	f.recordErr = errors.New("db down")

	if err := f.resolver().require(context.Background(), "vault-a", "card-1"); err != nil {
		t.Fatalf("require() = %v, want nil", err)
	}
}

func TestRequireDeniesWhenReapDisagrees(t *testing.T) {
	f := newFakeOwnership()
	f.remote["card-1"] = "user-b"

	if err := f.resolver().require(context.Background(), "vault-a", "card-1"); !errors.Is(err, ErrResourceNotOwned) {
		t.Fatalf("require() = %v, want ErrResourceNotOwned", err)
	}
	if f.recordCalls != 0 {
		t.Fatalf("record calls = %d, want 0", f.recordCalls)
	}
}

// Neither side knows the resource (REAP 404s, so reapOwner returns ""):
// denied, and nothing is recorded.
func TestRequireDeniesUnknownResource(t *testing.T) {
	f := newFakeOwnership()

	if err := f.resolver().require(context.Background(), "vault-a", "card-1"); !errors.Is(err, ErrResourceNotOwned) {
		t.Fatalf("require() = %v, want ErrResourceNotOwned", err)
	}
	if f.recordCalls != 0 {
		t.Fatalf("record calls = %d, want 0", f.recordCalls)
	}
}

// The conflict case: the local row names another vault but REAP says the
// caller owns it. Denied, and the row is left as it was — rewriting it on
// REAP's word would let a caller take over another vault's card.
func TestRequireDeniesConflictWithoutRewriting(t *testing.T) {
	f := newFakeOwnership()
	f.local["card-1"] = "vault-b"
	f.remote["card-1"] = "user-a"

	if err := f.resolver().require(context.Background(), "vault-a", "card-1"); !errors.Is(err, ErrResourceNotOwned) {
		t.Fatalf("require() = %v, want ErrResourceNotOwned", err)
	}
	if got := f.local["card-1"]; got != "vault-b" {
		t.Fatalf("local owner = %q, want it left at vault-b", got)
	}
	if f.recordCalls != 0 {
		t.Fatalf("record calls = %d, want 0", f.recordCalls)
	}
}

func TestRequireDeniesLocalMismatch(t *testing.T) {
	f := newFakeOwnership()
	f.local["card-1"] = "vault-b"
	f.remote["card-1"] = "user-b"

	if err := f.resolver().require(context.Background(), "vault-a", "card-1"); !errors.Is(err, ErrResourceNotOwned) {
		t.Fatalf("require() = %v, want ErrResourceNotOwned", err)
	}
}

// A vault with no REAP user can't own anything in REAP, so it gets a plain
// denial rather than the ErrNoReapUser the create/list endpoints report.
func TestRequireDeniesVaultWithoutReapUser(t *testing.T) {
	f := newFakeOwnership()
	f.remote["card-1"] = "user-a"

	if err := f.resolver().require(context.Background(), "vault-unmapped", "card-1"); !errors.Is(err, ErrResourceNotOwned) {
		t.Fatalf("require() = %v, want ErrResourceNotOwned", err)
	}
}

// 4xx (bar 429) is REAP refusing to hand us the resource — a denial, since
// the IDs reaching here are caller-supplied and a malformed one draws a
// 400/422. 429 and 5xx mean REAP can't answer right now, and must reach the
// caller as an error, or an outage would deny every heal attempt as "not
// yours".
func TestReapResourceFound(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		wantFound bool
		wantErr   bool
	}{
		{"ok", 200, true, false},
		{"not found", 404, false, false},
		{"bad request", 400, false, false},
		{"unauthorized", 401, false, false},
		{"unprocessable", 422, false, false},
		{"rate limited", 429, false, true},
		{"upstream error", 502, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, err := reapResourceFound("card", "card-1", tc.status, nil)
			if found != tc.wantFound || (err != nil) != tc.wantErr {
				t.Fatalf("reapResourceFound(%d) = %v, %v; want %v, err=%v", tc.status, found, err, tc.wantFound, tc.wantErr)
			}
		})
	}

	if _, err := reapResourceFound("card", "card-1", 0, errors.New("dial failed")); err == nil {
		t.Fatal("transport error must be returned")
	}
}

// A database or REAP failure must reach the handler as itself (a 5xx), not
// be flattened into "this isn't yours" — that's the whole point of
// reapResourceFound, and it only holds if require propagates the error.
func TestRequirePropagatesLookupErrors(t *testing.T) {
	dbDown := errors.New("db down")
	f := newFakeOwnership()
	f.localErr = dbDown
	if err := f.resolver().require(context.Background(), "vault-a", "card-1"); !errors.Is(err, dbDown) {
		t.Fatalf("require() = %v, want the local lookup error", err)
	}

	reapDown := errors.New("reap unreachable")
	f = newFakeOwnership()
	f.remoteErr = reapDown
	if err := f.resolver().require(context.Background(), "vault-a", "card-1"); !errors.Is(err, reapDown) {
		t.Fatalf("require() = %v, want the REAP lookup error", err)
	}
}

func TestRequireDeniesEmptyID(t *testing.T) {
	f := newFakeOwnership()

	if err := f.resolver().require(context.Background(), "vault-a", ""); !errors.Is(err, ErrResourceNotOwned) {
		t.Fatalf("require() = %v, want ErrResourceNotOwned", err)
	}
	if f.localCalls != 0 || f.remoteCalls != 0 {
		t.Fatalf("lookups = %d local / %d remote, want 0/0", f.localCalls, f.remoteCalls)
	}
}
