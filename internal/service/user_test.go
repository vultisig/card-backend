package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vultisig/card-backend/internal/reap"
)

const (
	sqlClaim  = "make_interval"            // ClaimReapUserCreate
	sqlRecord = "SET reap_user_id = $2"    // SetReapUserID
	sqlLookup = "SELECT reap_user_id FROM" // GetReapUserID
	// The only plain UPDATE; matching on the column it nulls would also hit SetReapUserID.
	sqlRelease = "UPDATE vultisig_reap_mappings"
)

// A reapmapping.Querier has no Begin, so a UserService holding one cannot open a transaction.
type fakeQuerier struct {
	claimGranted bool
	existingID   string
	statements   []string
}

func (f *fakeQuerier) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.statements = append(f.statements, sql)
	if strings.Contains(sql, sqlClaim) {
		if f.claimGranted {
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}
		return pgconn.NewCommandTag("INSERT 0 0"), nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *fakeQuerier) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	f.statements = append(f.statements, sql)
	if f.existingID == "" {
		return fakeRow{}
	}
	return fakeRow{id: &f.existingID}
}

func (f *fakeQuerier) ran(marker string) bool {
	for _, sql := range f.statements {
		if strings.Contains(sql, marker) {
			return true
		}
	}
	return false
}

type fakeRow struct{ id *string }

func (r fakeRow) Scan(dest ...any) error {
	*(dest[0].(**string)) = r.id
	return nil
}

func fakeReap(t *testing.T, db *fakeQuerier, status int, body string) (*reap.Client, *[]string) {
	t.Helper()
	var atCallTime []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atCallTime = append([]string(nil), db.statements...)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return reap.NewClientWithBaseURL(srv.URL, "test-key", nil), &atCallTime
}

// The point of the whole change: claim written, REAP called, result recorded — nothing else in between.
func TestCreateUserCallsReapBetweenTwoShortWrites(t *testing.T) {
	db := &fakeQuerier{claimGranted: true}
	client, atCallTime := fakeReap(t, db, http.StatusCreated, `{"id":"reap-user-1"}`)
	svc := NewUserService(db, client)

	status, _, err := svc.CreateUser(context.Background(), "pubkey", reap.CreateUserRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201", status)
	}

	if len(*atCallTime) != 1 || !strings.Contains((*atCallTime)[0], sqlClaim) {
		t.Fatalf("statements at REAP call time = %v, want just the claim", *atCallTime)
	}
	if !db.ran(sqlRecord) {
		t.Error("the REAP user ID was never recorded")
	}
	if db.ran(sqlRelease) {
		t.Error("released the claim on a successful create")
	}
}

// A rejected create must hand the claim back, or a retry waits out the stale window.
func TestCreateUserReleasesClaimOnReapError(t *testing.T) {
	db := &fakeQuerier{claimGranted: true}
	client, _ := fakeReap(t, db, http.StatusBadRequest, `{"error":"invalid email"}`)
	svc := NewUserService(db, client)

	status, body, err := svc.CreateUser(context.Background(), "pubkey", reap.CreateUserRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusBadRequest || !strings.Contains(string(body), "invalid email") {
		t.Fatalf("got %d %s, want REAP's 400 passed through", status, body)
	}
	if !db.ran(sqlRelease) {
		t.Error("the claim was not released after REAP rejected the create")
	}
	if db.ran(sqlRecord) {
		t.Error("recorded a REAP user ID for a create REAP rejected")
	}
}

// A 2xx with no id is not the same situation: REAP said it created the user, so
// there is nothing to record but also nothing safe to retry.
func TestCreateUserKeepsClaimOnUnusableResponse(t *testing.T) {
	db := &fakeQuerier{claimGranted: true}
	client, _ := fakeReap(t, db, http.StatusCreated, `{"no-id-here":true}`)
	svc := NewUserService(db, client)

	if _, _, err := svc.CreateUser(context.Background(), "pubkey", reap.CreateUserRequest{}); err == nil {
		t.Fatal("expected an error for a response with no id")
	}
	if db.ran(sqlRelease) {
		t.Error("the claim was released after a 2xx, letting a retry create a second REAP user")
	}
	if db.ran(sqlRecord) {
		t.Error("recorded a REAP user ID from a response that had none")
	}
}

// The nil REAP client is the assertion: reaching it panics.
func TestCreateUserRefusedClaim(t *testing.T) {
	tests := []struct {
		name       string
		existingID string
		want       error
	}{
		{"vault already has a reap user", "reap-user-1", ErrReapUserExists},
		{"another create is in flight", "", ErrReapUserCreateInFlight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &fakeQuerier{claimGranted: false, existingID: tt.existingID}
			svc := NewUserService(db, nil)

			_, _, err := svc.CreateUser(context.Background(), "pubkey", reap.CreateUserRequest{})
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if db.ran(sqlRelease) {
				t.Error("released a claim it never held")
			}
			if !db.ran(sqlLookup) {
				t.Error("did not check the row to tell the two refusals apart")
			}
		})
	}
}

// The claim must come first, so a concurrent create is excluded for the whole round trip.
func TestCreateUserClaimsFirst(t *testing.T) {
	db := &fakeQuerier{claimGranted: false}
	svc := NewUserService(db, nil)

	if _, _, err := svc.CreateUser(context.Background(), "pubkey", reap.CreateUserRequest{}); err == nil {
		t.Fatal("expected a refused claim to return an error")
	}
	if len(db.statements) == 0 || !strings.Contains(db.statements[0], sqlClaim) {
		t.Fatalf("first statement = %v, want the create claim", db.statements)
	}
}
