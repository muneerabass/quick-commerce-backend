package services

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var ErrInvalidAccessToken = errors.New("invalid access token")

type AccessClaims struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	ExpiresAt time.Time
}

// quickCommerceClaims is the registered+private claims set for access tokens.
type quickCommerceClaims struct {
	jwt.RegisteredClaims
	SessionID string `json:"sid"`
}

type TokenService struct {
	secret []byte
	ttl    time.Duration
}

func NewTokenService(secret string, ttl time.Duration) *TokenService {
	return &TokenService{secret: []byte(secret), ttl: ttl}
}

// Issue mints a signed HS256 JWT. The issuer is "quick-commerce"; alg:none and
// all other algorithms are rejected by Parse via WithValidMethods.
func (s *TokenService) Issue(userID, sessionID uuid.UUID, now time.Time) (string, error) {
	claims := quickCommerceClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "quick-commerce",
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
		SessionID: sessionID.String(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// Parse validates the token's algorithm (HS256 only), signature, expiry and
// issuer, then returns the extracted claims. alg:none, tampered signatures,
// wrong keys, expired tokens, and unexpected issuers all return
// ErrInvalidAccessToken.
func (s *TokenService) Parse(token string, now time.Time) (AccessClaims, error) {
	var claims quickCommerceClaims
	_, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		return s.secret, nil
	},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuedAt(),
		jwt.WithIssuer("quick-commerce"),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	sessionID, err := uuid.Parse(claims.SessionID)
	if err != nil {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	return AccessClaims{UserID: userID, SessionID: sessionID, ExpiresAt: exp.Time}, nil
}

func hashRefreshToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", digest[:])
}
