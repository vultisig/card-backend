package notification

import "testing"

// TestVaultID checks against the same test vector as the SDK's
// computeNotificationVaultId (packages/sdk/tests/unit/utils/computeNotificationVaultId.test.ts),
// so the two implementations stay in agreement.
func TestVaultID(t *testing.T) {
	got := VaultID("04testEcdsaPubKeyHex", "00112233445566778899aabbccddeeff")
	want := "456168d997f217cd775b746980ec0b41ae48660bab1e8334c10209a6ea6564cc"
	if got != want {
		t.Fatalf("VaultID() = %q, want %q", got, want)
	}
}

func TestNewClientRequiresRedisURL(t *testing.T) {
	if _, err := NewClient(""); err == nil {
		t.Fatal("NewClient(\"\") should return an error")
	}
	if _, err := NewClient("not a redis url"); err == nil {
		t.Fatal("NewClient() with an unparseable url should return an error")
	}
}

func TestNewClientParsesValidURL(t *testing.T) {
	c, err := NewClient("redis://localhost:6379/0")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer func() { _ = c.Close() }()
}
