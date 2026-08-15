package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
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

func TestValidatePublicKey(t *testing.T) {
	priv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	canonical := hex.EncodeToString(priv.PubKey().SerializeCompressed())

	if err := ValidatePublicKey(canonical); err != nil {
		t.Fatalf("expected canonical key to be accepted, got %v", err)
	}

	letter := strings.IndexAny(canonical, "abcdef")
	if letter < 0 {
		t.Skip("generated key has no hex letter to re-case")
	}

	// The same key, spelled four other ways.
	sameKey := map[string]string{
		"uppercase":    strings.ToUpper(canonical),
		"one char":     canonical[:letter] + strings.ToUpper(canonical[letter:letter+1]) + canonical[letter+1:],
		"0x prefix":    "0x" + canonical,
		"uncompressed": hex.EncodeToString(priv.PubKey().SerializeUncompressed()),
	}
	for name, spelling := range sameKey {
		raw, err := hex.DecodeString(strings.TrimPrefix(spelling, "0x"))
		if err != nil {
			t.Fatalf("%s: not valid hex", name)
		}
		parsed, err := secp256k1.ParsePubKey(raw)
		if err != nil || !parsed.IsEqual(priv.PubKey()) {
			t.Fatalf("%s: expected the same key to parse out", name)
		}
		if err := ValidatePublicKey(spelling); !errors.Is(err, ErrInvalidPublicKey) {
			t.Fatalf("%s: expected ErrInvalidPublicKey, got %v", name, err)
		}
	}

	malformed := map[string]string{
		"empty":             "",
		"too short":         canonical[:64],
		"non-hex":           strings.Repeat("z", 66),
		"not a curve point": strings.Repeat("ff", 33),
	}
	for name, publicKey := range malformed {
		if err := ValidatePublicKey(publicKey); !errors.Is(err, ErrInvalidPublicKey) {
			t.Fatalf("%s: expected ErrInvalidPublicKey, got %v", name, err)
		}
	}
}

func TestAuthenticate(t *testing.T) {
	priv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	canonical := hex.EncodeToString(priv.PubKey().SerializeCompressed())

	const nonce = int64(0)
	hash := sha256.Sum256([]byte(strconv.FormatInt(nonce, 10)))
	sigHex := hex.EncodeToString(ecdsa.Sign(priv, hash[:]).Serialize())

	// The nil pool asserts the key is rejected before any DB call.
	a := NewAuthService("test-secret", nil)
	_, err = a.Authenticate(context.Background(), strings.ToUpper(canonical), nonce, sigHex)
	if !errors.Is(err, ErrInvalidPublicKey) {
		t.Fatalf("expected ErrInvalidPublicKey, got %v", err)
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
