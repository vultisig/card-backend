package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/vultisig/card-backend/internal/bip32"
	"github.com/vultisig/card-backend/internal/reapmapping"
	"github.com/vultisig/card-backend/internal/vaulttoken"
)

const accessTokenDuration = 24 * time.Hour

var (
	ErrInvalidSignature = errors.New("invalid signature")
	ErrNonceUsed        = errors.New("nonce already used")
)

type Claims struct {
	jwt.RegisteredClaims
	PublicKey string `json:"public_key"`
}

type AuthService struct {
	jwtSecret []byte
	pool      *pgxpool.Pool
}

func NewAuthService(jwtSecret string, pool *pgxpool.Pool) *AuthService {
	return &AuthService{jwtSecret: []byte(jwtSecret), pool: pool}
}

// Authenticate verifies that signature is a valid secp256k1 signature (DER,
// hex-encoded) over nonce, made by the vault's unhardened ETH key
// (bip32.ETHPath, derived from the vault's root key publicKey and
// chainCodeHex), and that nonce is publicKey's next expected nonce per its
// VultisigReapMapping row (replay protection; a vault with no row yet has
// an implicit nonce of 0). On success it issues a JWT access token — keyed
// on the root publicKey, the vault's persistent identity — and advances the
// nonce, creating the mapping row on first use.
func (a *AuthService) Authenticate(ctx context.Context, publicKey string, chainCodeHex string, nonce int64, signatureHex string) (string, error) {
	currentNonce, err := reapmapping.GetNonce(ctx, a.pool, publicKey)
	if err != nil {
		return "", err
	}
	if nonce != currentNonce {
		return "", ErrNonceUsed
	}

	if !verifySignature(publicKey, chainCodeHex, nonce, signatureHex) {
		return "", ErrInvalidSignature
	}

	claimed, err := reapmapping.ClaimNonce(ctx, a.pool, publicKey, nonce)
	if err != nil {
		return "", err
	}
	if !claimed {
		return "", ErrNonceUsed
	}

	tokenID, err := newTokenID()
	if err != nil {
		return "", err
	}
	now := time.Now()
	expiresAt := now.Add(accessTokenDuration)
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        tokenID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		PublicKey: publicKey,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(a.jwtSecret)
	if err != nil {
		return "", err
	}

	if err := vaulttoken.Record(ctx, a.pool, tokenID, publicKey, expiresAt); err != nil {
		return "", err
	}
	return signed, nil
}

// newTokenID generates a random JWT ID (jti) for an issued access token, so
// it can be looked up in vault_tokens independent of its signature.
func newTokenID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// verifySignature reports whether signatureHex (DER-encoded secp256k1
// signature, hex) is a valid signature over nonce made by the unhardened
// ETH key (bip32.ETHPath) derived from root public key publicKey (hex) and
// chain code chainCodeHex (hex) — not by publicKey itself, since the
// client signs with its derived ETH private key, never the root key.
func verifySignature(publicKey, chainCodeHex string, nonce int64, signatureHex string) bool {
	pubKeyBytes, err := hex.DecodeString(strings.TrimPrefix(publicKey, "0x"))
	if err != nil {
		return false
	}
	rootPubKey, err := secp256k1.ParsePubKey(pubKeyBytes)
	if err != nil {
		return false
	}
	chainCodeBytes, err := hex.DecodeString(strings.TrimPrefix(chainCodeHex, "0x"))
	if err != nil || len(chainCodeBytes) != 32 {
		return false
	}
	root := &bip32.ExtendedPublicKey{Pub: rootPubKey}
	copy(root.ChainCode[:], chainCodeBytes)
	ethKey, err := root.DerivePath(bip32.ETHPath)
	if err != nil {
		return false
	}

	sigBytes, err := hex.DecodeString(strings.TrimPrefix(signatureHex, "0x"))
	if err != nil {
		return false
	}
	sig, err := ecdsa.ParseDERSignature(sigBytes)
	if err != nil {
		return false
	}
	hash := sha256.Sum256([]byte(strconv.FormatInt(nonce, 10)))
	return sig.Verify(hash[:], ethKey.Pub)
}

func (a *AuthService) ValidateToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return a.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid or expired token")
	}
	return claims, nil
}

// RequireAuth is Echo middleware that validates the Bearer JWT, checks it
// against its vault_tokens record (rejecting tokens that were never issued
// by this server, e.g. a stale DB, or have since been revoked), and stores
// the resulting Claims on the context (key "claims") for handlers to use.
func (a *AuthService) RequireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		authHeader := c.Request().Header.Get("Authorization")
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenStr == "" || tokenStr == authHeader {
			return echo.NewHTTPError(401, "missing bearer token")
		}
		claims, err := a.ValidateToken(tokenStr)
		if err != nil {
			return echo.NewHTTPError(401, "invalid or expired token")
		}

		tok, err := vaulttoken.Touch(c.Request().Context(), a.pool, claims.ID)
		if err != nil {
			log.Printf("auth: check token: %v", err)
			return echo.NewHTTPError(500, "internal error")
		}
		if tok == nil || tok.IsRevoked() || tok.ExpiresAt.Before(time.Now()) {
			return echo.NewHTTPError(401, "invalid or expired token")
		}

		c.Set("claims", claims)
		return next(c)
	}
}
