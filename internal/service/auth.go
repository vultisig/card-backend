package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/vultisig/card-backend/internal/card"
)

const accessTokenDuration = 24 * time.Hour

var (
	ErrCardNotActive    = errors.New("card not active")
	ErrInvalidSignature = errors.New("invalid signature")
	ErrNonceUsed        = errors.New("nonce already used")
)

type Claims struct {
	jwt.RegisteredClaims
	CardID    string `json:"card_id"`
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
// hex-encoded) over nonce, made by the vault key registered for publicKey,
// and that nonce is the card's next expected nonce (replay protection).
// On success it issues a JWT access token and advances the card's nonce.
func (a *AuthService) Authenticate(ctx context.Context, publicKey string, nonce int64, signatureHex string) (string, error) {
	c, err := card.GetByPublicKey(ctx, a.pool, publicKey)
	if err != nil {
		return "", err
	}
	if !c.IsActive {
		return "", ErrCardNotActive
	}
	if nonce != c.Nonce {
		return "", ErrNonceUsed
	}

	if !verifySignature(publicKey, nonce, signatureHex) {
		return "", ErrInvalidSignature
	}

	claimed, err := card.ClaimNonce(ctx, a.pool, c.CardID, nonce)
	if err != nil {
		return "", err
	}
	if !claimed {
		return "", ErrNonceUsed
	}

	now := time.Now()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenDuration)),
		},
		CardID:    c.CardID,
		PublicKey: c.VaultPublicKeyECDSA,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.jwtSecret)
}

// verifySignature reports whether signatureHex (DER-encoded secp256k1
// signature, hex) is a valid signature over nonce made by publicKey (hex).
func verifySignature(publicKey string, nonce int64, signatureHex string) bool {
	pubKeyBytes, err := hex.DecodeString(strings.TrimPrefix(publicKey, "0x"))
	if err != nil {
		return false
	}
	pubKey, err := secp256k1.ParsePubKey(pubKeyBytes)
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
	return sig.Verify(hash[:], pubKey)
}

func (a *AuthService) ValidateToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
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

// RequireAuth is Echo middleware that validates the Bearer JWT and stores
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
		c.Set("claims", claims)
		return next(c)
	}
}
