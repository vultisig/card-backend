package reap

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestClientAdvanceUserApplication(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/users/user-1/application" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Idempotency-Key"); got != "test-idempotency-key" {
			t.Errorf("Idempotency-Key = %q, want test-idempotency-key", got)
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"method":"MANAGED_KYC"}` {
			t.Errorf("body = %s, want {\"method\":\"MANAGED_KYC\"}", b)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"IN_REVIEW","nextAction":null}`))
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "test-key", http: srv.Client()}

	status, body, err := c.AdvanceUserApplication(context.Background(), "user-1", []byte(`{"method":"MANAGED_KYC"}`), "test-idempotency-key")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"status":"IN_REVIEW","nextAction":null}` {
		t.Fatalf("AdvanceUserApplication = %d %s", status, body)
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

func TestClientCards(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/cards/":
			if got := r.Header.Get("Idempotency-Key"); got != "test-idempotency-key" {
				t.Errorf("Idempotency-Key = %q, want test-idempotency-key", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"card-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cards/":
			if got := r.URL.Query().Get("userId"); got != "user-1" {
				t.Errorf("userId = %q, want user-1", got)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[],"nextCursor":null}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cards/card-1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"card-1"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/cards/card-1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && r.URL.Path == "/cards/card-1/pin":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/cards/card-1/freeze":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"card-1","status":"FROZEN"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards/card-1/unfreeze":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"card-1","status":"ACTIVE"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards/card-1/reveal":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"revealUrl":"https://reveal"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/cards/card-1/3ds-challenge-method":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/cards/card-1/activate":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/cards/card-1/activation-code":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"activationCode":"123456"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards/card-1/push-provisioning":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"provider":"GOOGLE_PAY","opc":"opc","last4":"1234"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/card-3ds-challenges/chal-1/respond":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"chal-1","status":"APPROVED"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/card-3ds-challenges/chal-1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"chal-1","status":"PENDING"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "test-key", http: srv.Client()}

	status, body, err := c.CreateCard(context.Background(), CreateCardRequest{AccountID: "acc-1", Type: "VIRTUAL"}, "test-idempotency-key")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusCreated || string(body) != `{"id":"card-1"}` {
		t.Fatalf("CreateCard = %d %s", status, body)
	}

	status, body, err = c.ListCards(context.Background(), url.Values{"userId": {"user-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"items":[],"nextCursor":null}` {
		t.Fatalf("ListCards = %d %s", status, body)
	}

	status, body, err = c.GetCard(context.Background(), "card-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"card-1"}` {
		t.Fatalf("GetCard = %d %s", status, body)
	}

	status, body, err = c.DeleteCard(context.Background(), "card-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent || len(body) != 0 {
		t.Fatalf("DeleteCard = %d %s", status, body)
	}

	status, body, err = c.UpdateCardPin(context.Background(), "card-1", "1357")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent || len(body) != 0 {
		t.Fatalf("UpdateCardPin = %d %s", status, body)
	}

	status, _, err = c.FreezeCard(context.Background(), "card-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("FreezeCard = %d", status)
	}

	status, _, err = c.UnfreezeCard(context.Background(), "card-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("UnfreezeCard = %d", status)
	}

	status, body, err = c.RevealCardDetails(context.Background(), "card-1", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"revealUrl":"https://reveal"}` {
		t.Fatalf("RevealCardDetails = %d %s", status, body)
	}

	status, _, err = c.UpdateCard3DSChallengeMethod(context.Background(), "card-1", "WEBHOOK")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("UpdateCard3DSChallengeMethod = %d", status)
	}

	status, _, err = c.ActivatePhysicalCard(context.Background(), "card-1", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("ActivatePhysicalCard = %d", status)
	}

	status, body, err = c.GetCardActivationCode(context.Background(), "card-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"activationCode":"123456"}` {
		t.Fatalf("GetCardActivationCode = %d %s", status, body)
	}

	status, body, err = c.PushProvisionCard(context.Background(), "card-1", "GOOGLE_PAY", "wallet-1", "device-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"provider":"GOOGLE_PAY","opc":"opc","last4":"1234"}` {
		t.Fatalf("PushProvisionCard = %d %s", status, body)
	}

	status, body, err = c.RespondToCard3DSChallenge(context.Background(), "chal-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"chal-1","status":"APPROVED"}` {
		t.Fatalf("RespondToCard3DSChallenge = %d %s", status, body)
	}

	status, body, err = c.GetCard3DSChallenge(context.Background(), "chal-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"chal-1","status":"PENDING"}` {
		t.Fatalf("GetCard3DSChallenge = %d %s", status, body)
	}
}

func TestClientCardShipments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/card-shipments/":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"ship-1","status":"DRAFT"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/card-shipments/":
			if got := r.URL.Query().Get("cardId"); got != "card-1" {
				t.Errorf("cardId = %q, want card-1", got)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[],"nextCursor":null}`))
		case r.Method == http.MethodGet && r.URL.Path == "/card-shipments/ship-1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"ship-1","status":"DRAFT"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/card-shipments/ship-1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPatch && r.URL.Path == "/card-shipments/ship-1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"ship-1","status":"DRAFT"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/card-shipments/ship-1/cards":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"ship-1","status":"DRAFT"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/card-shipments/ship-1/cards/member-1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"ship-1","status":"DRAFT"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/card-shipments/ship-1/submit":
			if got := r.Header.Get("Idempotency-Key"); got != "test-idempotency-key" {
				t.Errorf("Idempotency-Key = %q, want test-idempotency-key", got)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"ship-1","status":"PLACED"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "test-key", http: srv.Client()}

	status, body, err := c.CreateCardShipment(context.Background(), CreateCardShipmentRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"ship-1","status":"DRAFT"}` {
		t.Fatalf("CreateCardShipment = %d %s", status, body)
	}

	status, body, err = c.ListCardShipments(context.Background(), url.Values{"cardId": {"card-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"items":[],"nextCursor":null}` {
		t.Fatalf("ListCardShipments = %d %s", status, body)
	}

	status, body, err = c.GetCardShipment(context.Background(), "ship-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"ship-1","status":"DRAFT"}` {
		t.Fatalf("GetCardShipment = %d %s", status, body)
	}

	status, body, err = c.DeleteCardShipment(context.Background(), "ship-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent || len(body) != 0 {
		t.Fatalf("DeleteCardShipment = %d %s", status, body)
	}

	status, _, err = c.UpdateCardShipment(context.Background(), "ship-1", UpdateCardShipmentRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("UpdateCardShipment = %d", status)
	}

	status, _, err = c.AddCardToShipment(context.Background(), "ship-1", CardShipmentMember{CardID: "card-1"})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("AddCardToShipment = %d", status)
	}

	status, _, err = c.RemoveCardFromShipment(context.Background(), "ship-1", "member-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("RemoveCardFromShipment = %d", status)
	}

	status, body, err = c.SubmitCardShipment(context.Background(), "ship-1", "test-idempotency-key")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"ship-1","status":"PLACED"}` {
		t.Fatalf("SubmitCardShipment = %d %s", status, body)
	}
}

func TestClientCardDesigns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/card-designs/":
			if got := r.URL.Query().Get("status"); got != "ACTIVE" {
				t.Errorf("status = %q, want ACTIVE", got)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/card-designs/design-1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"design-1","name":"Classic"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "test-key", http: srv.Client()}

	status, body, err := c.ListCardDesigns(context.Background(), url.Values{"status": {"ACTIVE"}})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"items":[]}` {
		t.Fatalf("ListCardDesigns = %d %s", status, body)
	}

	status, body, err = c.GetCardDesign(context.Background(), "design-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"design-1","name":"Classic"}` {
		t.Fatalf("GetCardDesign = %d %s", status, body)
	}
}

func TestClientGetCardTransaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/card-transactions/txn-1" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"txn-1","status":"CLEARED"}`))
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "test-key", http: srv.Client()}

	status, body, err := c.GetCardTransaction(context.Background(), "txn-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"txn-1","status":"CLEARED"}` {
		t.Fatalf("GetCardTransaction = %d %s", status, body)
	}
}

func TestClientListActivities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/activities/" {
			if got := r.URL.Query().Get("accountId"); got != "acc-1" {
				t.Errorf("accountId = %q, want acc-1", got)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[],"nextCursor":null}`))
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "test-key", http: srv.Client()}

	status, body, err := c.ListActivities(context.Background(), url.Values{"accountId": {"acc-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"items":[],"nextCursor":null}` {
		t.Fatalf("ListActivities = %d %s", status, body)
	}
}

func TestClientFraudAlerts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/fraud-alerts/":
			if got := r.URL.Query().Get("cardId"); got != "card-1" {
				t.Errorf("cardId = %q, want card-1", got)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[],"nextCursor":null}`))
		case r.Method == http.MethodPost && r.URL.Path == "/fraud-alerts/":
			if got := r.Header.Get("Idempotency-Key"); got != "test-idempotency-key" {
				t.Errorf("Idempotency-Key = %q, want test-idempotency-key", got)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"alert-1","status":"CONFIRMED"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/fraud-alerts/alert-1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"alert-1","status":"PENDING"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/fraud-alerts/alert-1/respond":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"alert-1","status":"DECLINED"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "test-key", http: srv.Client()}

	status, body, err := c.ListFraudAlerts(context.Background(), url.Values{"cardId": {"card-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"items":[],"nextCursor":null}` {
		t.Fatalf("ListFraudAlerts = %d %s", status, body)
	}

	status, body, err = c.ReportFraud(context.Background(), ReportFraudRequest{TransactionID: "txn-1", Type: "CARD_LOST"}, "test-idempotency-key")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"alert-1","status":"CONFIRMED"}` {
		t.Fatalf("ReportFraud = %d %s", status, body)
	}

	status, body, err = c.GetFraudAlert(context.Background(), "alert-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"alert-1","status":"PENDING"}` {
		t.Fatalf("GetFraudAlert = %d %s", status, body)
	}

	status, body, err = c.RespondToFraudAlert(context.Background(), "alert-1", RespondToFraudAlertRequest{Response: "DECLINED"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"alert-1","status":"DECLINED"}` {
		t.Fatalf("RespondToFraudAlert = %d %s", status, body)
	}
}

func TestClientSimulation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/simulation/users/user-1/application":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/simulation/companies/company-1/status":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/simulation/accounts/acc-1/status":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/simulation/cards/card-1/status":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/simulation/card-transactions/authorization":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"txn-1","status":"PENDING"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/simulation/card-transactions/authorization-with-3ds":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"chal-1","status":"PENDING"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/simulation/card-transactions/decline":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"txn-2","status":"DECLINED"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/simulation/card-transactions/clearing":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"txn-1","status":"CLEARED"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/simulation/card-transactions/reversal":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"txn-1","status":"VOID"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/simulation/card-transactions/refund":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"txn-3","status":"CLEARED"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "test-key", http: srv.Client()}

	status, body, err := c.SimulateUserApplicationStatus(context.Background(), "user-1", "APPROVED")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent || len(body) != 0 {
		t.Fatalf("SimulateUserApplicationStatus = %d %s", status, body)
	}

	status, _, err = c.SimulateCompanyStatus(context.Background(), "company-1", SimulateCompanyStatusRequest{Status: "REJECTED"})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("SimulateCompanyStatus = %d", status)
	}

	status, _, err = c.SimulateAccountStatus(context.Background(), "acc-1", "RESTRICTED")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("SimulateAccountStatus = %d", status)
	}

	status, _, err = c.SimulateCardStatus(context.Background(), "card-1", "FROZEN")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("SimulateCardStatus = %d", status)
	}

	status, body, err = c.SimulateAuthorization(context.Background(), SimulateCardTransactionRequest{CardID: "card-1", Amount: 10})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"txn-1","status":"PENDING"}` {
		t.Fatalf("SimulateAuthorization = %d %s", status, body)
	}

	status, body, err = c.SimulateThreeDSAuthorization(context.Background(), SimulateCardTransactionRequest{CardID: "card-1", Amount: 10})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"chal-1","status":"PENDING"}` {
		t.Fatalf("SimulateThreeDSAuthorization = %d %s", status, body)
	}

	status, body, err = c.SimulateDecline(context.Background(), SimulateCardTransactionRequest{CardID: "card-1", Amount: 10})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"txn-2","status":"DECLINED"}` {
		t.Fatalf("SimulateDecline = %d %s", status, body)
	}

	status, body, err = c.SimulateClearing(context.Background(), SimulateCardTransactionRequest{CardID: "card-1", Amount: 10, TransactionID: "txn-1"})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"txn-1","status":"CLEARED"}` {
		t.Fatalf("SimulateClearing = %d %s", status, body)
	}

	status, body, err = c.SimulateReversal(context.Background(), SimulateCardTransactionRequest{CardID: "card-1", Amount: 10, TransactionID: "txn-1"})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"txn-1","status":"VOID"}` {
		t.Fatalf("SimulateReversal = %d %s", status, body)
	}

	status, body, err = c.SimulateRefund(context.Background(), SimulateCardTransactionRequest{CardID: "card-1", Amount: 10})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"id":"txn-3","status":"CLEARED"}` {
		t.Fatalf("SimulateRefund = %d %s", status, body)
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
