package bip32

import (
	"crypto/rand"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func randomMaster(t *testing.T) *ExtendedPrivateKey {
	t.Helper()
	priv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	var chainCode [32]byte
	if _, err := rand.Read(chainCode[:]); err != nil {
		t.Fatal(err)
	}
	return &ExtendedPrivateKey{Priv: priv, ChainCode: chainCode}
}

func (k *ExtendedPrivateKey) public() *ExtendedPublicKey {
	return &ExtendedPublicKey{Pub: k.Priv.PubKey(), ChainCode: k.ChainCode}
}

// TestExtendedPublicKey_Child_MatchesPrivateDerivation checks BIP32's core
// property: CKDpub(parent pubkey, i) == pubkey(CKDpriv(parent privkey, i))
// for unhardened i. This is what makes deriving the ETH pubkey server-side
// from just the root pubkey + chain code (no private key) valid.
func TestExtendedPublicKey_Child_MatchesPrivateDerivation(t *testing.T) {
	master := randomMaster(t)

	for _, index := range []uint32{0, 1, 44, 60, HardenedOffset - 1} {
		wantPriv, err := master.Child(index)
		if err != nil {
			t.Fatalf("index %d: private derivation: %v", index, err)
		}
		gotPub, err := master.public().Child(index)
		if err != nil {
			t.Fatalf("index %d: public derivation: %v", index, err)
		}
		if !gotPub.Pub.IsEqual(wantPriv.Priv.PubKey()) {
			t.Errorf("index %d: public-derived key does not match private-derived key's pubkey", index)
		}
		if gotPub.ChainCode != wantPriv.ChainCode {
			t.Errorf("index %d: public-derived chain code does not match private-derived chain code", index)
		}
	}
}

// TestExtendedPublicKey_DerivePath_MatchesETHPath walks the full ETHPath
// (m/44/60/0/0/0) via CKDpub and checks it lands on the same key as walking
// it via CKDpriv, exactly like the server (public-only) and vaultkey CLI
// (private) must agree in practice.
func TestExtendedPublicKey_DerivePath_MatchesETHPath(t *testing.T) {
	master := randomMaster(t)

	wantPriv, err := master.DerivePath(ETHPath)
	if err != nil {
		t.Fatal(err)
	}
	gotPub, err := master.public().DerivePath(ETHPath)
	if err != nil {
		t.Fatal(err)
	}
	if !gotPub.Pub.IsEqual(wantPriv.Priv.PubKey()) {
		t.Fatal("public-derived ETH key does not match private-derived ETH key's pubkey")
	}
}

func TestExtendedPublicKey_Child_RejectsHardened(t *testing.T) {
	master := randomMaster(t)

	if _, err := master.public().Child(HardenedOffset); err == nil {
		t.Fatal("expected error deriving a hardened child from a public key")
	}
}

func TestExtendedPublicKey_Child_DifferentIndicesDiverge(t *testing.T) {
	master := randomMaster(t)

	a, err := master.public().Child(0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := master.public().Child(1)
	if err != nil {
		t.Fatal(err)
	}
	if a.Pub.IsEqual(b.Pub) {
		t.Fatal("different indices must derive different public keys")
	}
}
