package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/golang-jwt/jwt/v5"
)

func TestVerifySignature(t *testing.T) {
	priv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	pubKeyHex := hex.EncodeToString(priv.PubKey().SerializeCompressed())

	const nonce = int64(7)
	hash := sha256.Sum256([]byte(strconv.FormatInt(nonce, 10)))
	sig := ecdsa.Sign(priv, hash[:])
	sigHex := hex.EncodeToString(sig.Serialize())

	if !verifySignature(pubKeyHex, nonce, sigHex) {
		t.Fatal("expected valid signature to verify")
	}
	if verifySignature(pubKeyHex, nonce+1, sigHex) {
		t.Fatal("signature must not verify against a different nonce")
	}
	other, _ := secp256k1.GeneratePrivateKey()
	otherPubKeyHex := hex.EncodeToString(other.PubKey().SerializeCompressed())
	if verifySignature(otherPubKeyHex, nonce, sigHex) {
		t.Fatal("signature must not verify against a different public key")
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
