package reap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientCreateAndGetUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		if got := r.Header.Get("Reap-Version"); got != apiVersion {
			t.Errorf("Reap-Version = %q, want %q", got, apiVersion)
		}

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/users/":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"user-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/users/user-1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"user-1","email":"a@b.com"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/users/user-1/email":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && r.URL.Path == "/users/user-1/phone":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "test-key", http: srv.Client()}

	status, body, err := c.CreateUser(context.Background(), CreateUserRequest{Email: "a@b.com"})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusCreated || string(body) != `{"id":"user-1"}` {
		t.Fatalf("CreateUser = %d %s", status, body)
	}

	status, body, err = c.GetUser(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"user-1","email":"a@b.com"}` {
		t.Fatalf("GetUser = %d %s", status, body)
	}

	status, body, err = c.UpdateEmail(context.Background(), "user-1", "new@b.com")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent || len(body) != 0 {
		t.Fatalf("UpdateEmail = %d %s", status, body)
	}

	status, body, err = c.UpdatePhoneNumber(context.Background(), "user-1", "+14155552671")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent || len(body) != 0 {
		t.Fatalf("UpdatePhoneNumber = %d %s", status, body)
	}
}

func TestClientAccounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/accounts/":
			if got := r.Header.Get("Idempotency-Key"); got != "test-idempotency-key" {
				t.Errorf("Idempotency-Key = %q, want test-idempotency-key", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"acc-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/auth-message":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message":"sign me"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/acc-1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"acc-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/acc-1/balance":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"currency":"USD"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/acc-1/assets":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/":
			if got := r.URL.Query().Get("ownerId"); got != "user-1" {
				t.Errorf("ownerId = %q, want user-1", got)
			}
			if got := r.URL.Query().Get("limit"); got != "5" {
				t.Errorf("limit = %q, want 5", got)
			}
			if got := r.URL.Query().Get("cursor"); got != "next" {
				t.Errorf("cursor = %q, want next", got)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[],"nextCursor":null}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "test-key", http: srv.Client()}

	status, body, err := c.CreateAccount(context.Background(), CreateAccountRequest{OwnerID: "user-1"}, "test-idempotency-key")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusCreated || string(body) != `{"id":"acc-1"}` {
		t.Fatalf("CreateAccount = %d %s", status, body)
	}

	status, body, err = c.GenerateSignerMessage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"message":"sign me"}` {
		t.Fatalf("GenerateSignerMessage = %d %s", status, body)
	}

	status, body, err = c.GetAccount(context.Background(), "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"acc-1"}` {
		t.Fatalf("GetAccount = %d %s", status, body)
	}

	status, body, err = c.GetAccountBalance(context.Background(), "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"currency":"USD"}` {
		t.Fatalf("GetAccountBalance = %d %s", status, body)
	}

	status, body, err = c.GetAccountAssets(context.Background(), "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"items":[]}` {
		t.Fatalf("GetAccountAssets = %d %s", status, body)
	}

	status, body, err = c.ListAccounts(context.Background(), "user-1", 5, "next")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"items":[],"nextCursor":null}` {
		t.Fatalf("ListAccounts = %d %s", status, body)
	}
}

func TestNewClientBaseURL(t *testing.T) {
	if got := NewClient(EnvProd, "k").baseURL; got != prodBaseURL {
		t.Errorf("prod baseURL = %q, want %q", got, prodBaseURL)
	}
	if got := NewClient("anything-else", "k").baseURL; got != sandboxBaseURL {
		t.Errorf("default baseURL = %q, want %q", got, sandboxBaseURL)
	}
}
