package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidAccessToken = errors.New("invalid access token")

type AccessClaims struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	ExpiresAt time.Time
}

type TokenService struct {
	secret []byte
	ttl    time.Duration
}

func NewTokenService(secret string, ttl time.Duration) *TokenService {
	return &TokenService{secret: []byte(secret), ttl: ttl}
}

func (s *TokenService) Issue(userID, sessionID uuid.UUID, now time.Time) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]any{
		"sub": userID.String(),
		"sid": sessionID.String(),
		"iat": now.Unix(),
		"exp": now.Add(s.ttl).Unix(),
		"iss": "quick-commerce",
	})
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	message := header + "." + encodedPayload
	signature := signJWT(s.secret, message)
	return message + "." + signature, nil
}

func (s *TokenService) Parse(token string, now time.Time) (AccessClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	want := signJWT(s.secret, parts[0]+"."+parts[1])
	if subtle.ConstantTimeCompare([]byte(want), []byte(parts[2])) != 1 {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	var claims struct {
		Subject string `json:"sub"`
		Session string `json:"sid"`
		Issued  int64  `json:"iat"`
		Expires int64  `json:"exp"`
		Issuer  string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Issuer != "quick-commerce" {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	sessionID, err := uuid.Parse(claims.Session)
	if err != nil || claims.Issued <= 0 || claims.Expires <= claims.Issued || now.Unix() >= claims.Expires {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	return AccessClaims{UserID: userID, SessionID: sessionID, ExpiresAt: time.Unix(claims.Expires, 0)}, nil
}

func signJWT(secret []byte, message string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(message))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func hashRefreshToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", digest[:])
}
