package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/golang-jwt/jwt/v5"

	"github.com/vultisig/card-backend/internal/bip32"
)

func TestVerifySignature(t *testing.T) {
	rootPriv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	var chainCode [32]byte
	if _, err := rand.Read(chainCode[:]); err != nil {
		t.Fatal(err)
	}
	root := &bip32.ExtendedPrivateKey{Priv: rootPriv, ChainCode: chainCode}
	ethKey, err := root.DerivePath(bip32.ETHPath)
	if err != nil {
		t.Fatal(err)
	}

	pubKeyHex := hex.EncodeToString(rootPriv.PubKey().SerializeCompressed())
	chainCodeHex := hex.EncodeToString(chainCode[:])

	const nonce = int64(7)
	hash := sha256.Sum256([]byte(strconv.FormatInt(nonce, 10)))
	sig := ecdsa.Sign(ethKey.Priv, hash[:])
	sigHex := hex.EncodeToString(sig.Serialize())

	if !verifySignature(pubKeyHex, chainCodeHex, nonce, sigHex) {
		t.Fatal("expected valid signature to verify")
	}
	if verifySignature(pubKeyHex, chainCodeHex, nonce+1, sigHex) {
		t.Fatal("signature must not verify against a different nonce")
	}
	other, _ := secp256k1.GeneratePrivateKey()
	otherPubKeyHex := hex.EncodeToString(other.PubKey().SerializeCompressed())
	if verifySignature(otherPubKeyHex, chainCodeHex, nonce, sigHex) {
		t.Fatal("signature must not verify against a different root public key")
	}
	otherChainCode := chainCode
	otherChainCode[0] ^= 0xff
	if verifySignature(pubKeyHex, hex.EncodeToString(otherChainCode[:]), nonce, sigHex) {
		t.Fatal("signature must not verify against a different chain code")
	}
	// Signing with the root key directly (instead of the derived ETH key)
	// must not verify — the whole point of this change.
	rootSig := ecdsa.Sign(rootPriv, hash[:])
	if verifySignature(pubKeyHex, chainCodeHex, nonce, hex.EncodeToString(rootSig.Serialize())) {
		t.Fatal("signature made with the root key must not verify")
	}
}

func TestValidateToken(t *testing.T) {
	a := &AuthService{jwtSecret: []byte("test-secret")}

	claims := &Claims{PublicKey: "pk"}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.jwtSecret)
	if err != nil {
		t.Fatal(err)
	}

	got, err := a.ValidateToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.PublicKey != "pk" {
		t.Fatalf("expected pk, got %s", got.PublicKey)
	}

	if _, err := a.ValidateToken(token + "tampered"); err == nil {
		t.Fatal("expected tampered token to fail validation")
	}
}
