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
}

func TestNewClientBaseURL(t *testing.T) {
	if got := NewClient(EnvProd, "k").baseURL; got != prodBaseURL {
		t.Errorf("prod baseURL = %q, want %q", got, prodBaseURL)
	}
	if got := NewClient("anything-else", "k").baseURL; got != sandboxBaseURL {
		t.Errorf("default baseURL = %q, want %q", got, sandboxBaseURL)
	}
}
