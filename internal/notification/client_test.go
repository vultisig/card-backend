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
