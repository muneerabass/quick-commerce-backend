package services

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/big"
	"regexp"
	"strings"
	"sync"
	"time"

	"quick-commerce/internal/domain"
	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrInvalidOTP          = errors.New("invalid OTP")
	ErrOTPExpired          = errors.New("OTP has expired")
	ErrOTPAttemptsExceeded = errors.New("OTP attempt limit exceeded")
	ErrOTPTooSoon          = errors.New("OTP was sent recently")
	ErrOTPRateLimited      = errors.New("OTP send rate limit exceeded")
	ErrOTPProvider         = errors.New("OTP provider is not configured")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrSessionRevoked      = errors.New("session is revoked or expired")
	ErrAccountDeleted      = errors.New("account is deleted")
	ErrInvalidInput        = errors.New("invalid input")
)

var otpPattern = regexp.MustCompile(`^[0-9]{6}$`)

type OTPSender interface {
	Send(ctx context.Context, phone, otp string) error
}

type UnconfiguredOTPSender struct{}

func (UnconfiguredOTPSender) Send(context.Context, string, string) error { return ErrOTPProvider }

// DevStdoutOTPSender is a dev-only sender that only logs the OTP to stdout and
// never returns an error. It is wired in SetupUserRoutes when DEV_OTP_LOG is true
// so that local development does not require a real OTP provider.
type DevStdoutOTPSender struct{}

func (DevStdoutOTPSender) Send(_ context.Context, phone, otp string) error {
	log.Printf("[DEV OTP] phone=%s otp=%s", phone, otp)
	return nil
}

type AuthService struct {
	db          *gorm.DB
	users       repository.UserRepository
	otps        repository.OTPRepository
	sessions    repository.SessionRepository
	sender      OTPSender
	token       *TokenService
	otpSecret   []byte
	otpTTL      time.Duration
	resendAfter time.Duration
	refreshTTL  time.Duration
	maxAttempts int
	adminPhone  string
	devOTPLog   bool
	lastOTPs    sync.Map // phone -> string (last plaintext OTP, dev only)
}

func (s *AuthService) SetDevOTPLog(enabled bool) { s.devOTPLog = enabled }

// LastDevOTP returns the most recent plaintext OTP generated for a phone, when
// dev-OTP logging is on. Returns ("", false) if there isn't one or the feature
// is disabled.
func (s *AuthService) LastDevOTP(rawPhone string) (string, bool) {
	if !s.devOTPLog {
		return "", false
	}
	phone, err := NormalizePhone(rawPhone)
	if err != nil {
		return "", false
	}
	value, ok := s.lastOTPs.Load(phone)
	if !ok {
		return "", false
	}
	return value.(string), true
}

func (s *AuthService) SetAdminPhone(phone string) error {
	if strings.TrimSpace(phone) == "" {
		s.adminPhone = ""
		return nil
	}
	normalized, err := NormalizePhone(phone)
	if err != nil {
		return err
	}
	s.adminPhone = normalized
	return nil
}

func (s *AuthService) UserRole(userID uuid.UUID) (string, error) {
	user, err := s.users.FindByID(nil, userID)
	if err != nil {
		return "", err
	}
	return user.Role, nil
}

func NewAuthService(db *gorm.DB, users repository.UserRepository, otps repository.OTPRepository, sessions repository.SessionRepository, sender OTPSender, jwtSecret, otpSecret string, accessTTL, refreshTTL, otpTTL, resendAfter time.Duration, maxAttempts int) *AuthService {
	return &AuthService{
		db: db, users: users, otps: otps, sessions: sessions, sender: sender,
		token: NewTokenService(jwtSecret, accessTTL), otpSecret: []byte(otpSecret),
		otpTTL: otpTTL, resendAfter: resendAfter, refreshTTL: refreshTTL, maxAttempts: maxAttempts,
	}
}

func (s *AuthService) TokenService() *TokenService { return s.token }

func (s *AuthService) ValidateAccessSession(userID, sessionID uuid.UUID) error {
	if err := s.sessions.FindActive(nil, sessionID, userID, time.Now().UTC()); err != nil {
		return ErrSessionRevoked
	}
	return nil
}

func (s *AuthService) SendOTP(ctx context.Context, rawPhone string) error {
	phone, err := NormalizePhone(rawPhone)
	if err != nil {
		return err
	}
	otp, err := generateOTP()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return s.db.Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", phone).Error; err != nil {
				return err
			}
		}
		latestCount, err := s.otps.CountRecent(tx, phone, now.Add(-time.Hour))
		if err != nil {
			return err
		}
		if latestCount >= 5 {
			return ErrOTPRateLimited
		}
		latest, latestErr := s.otps.LatestForUpdate(tx, phone)
		if latestErr == nil && now.Sub(latest.CreatedAt) < s.resendAfter {
			return ErrOTPTooSoon
		}
		if latestErr != nil && !errors.Is(latestErr, repository.ErrNotFound) {
			return latestErr
		}
		if err := s.sender.Send(ctx, phone, otp); err != nil {
			return err
		}
		if s.devOTPLog {
			s.lastOTPs.Store(phone, otp)
		}
		return s.otps.Create(tx, &domain.OTPVerification{
			Phone: phone, OTPHash: s.hashOTP(phone, otp),
			ExpiresAt: now.Add(s.otpTTL), Attempts: 0, CreatedAt: now,
		})
	})
}

func (s *AuthService) VerifyOTP(ctx context.Context, rawPhone, otp string) (dto.AuthResponse, error) {
	phone, err := NormalizePhone(rawPhone)
	if err != nil {
		return dto.AuthResponse{}, err
	}
	if !otpPattern.MatchString(otp) {
		return dto.AuthResponse{}, ErrInvalidOTP
	}
	now := time.Now().UTC()
	var user *domain.User
	var session *domain.UserSession
	var refreshToken string
	var verificationErr error
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", phone).Error; err != nil {
				return err
			}
		}
		verification, err := s.otps.LatestForUpdate(tx, phone)
		if errors.Is(err, repository.ErrNotFound) {
			verificationErr = ErrInvalidOTP
			return nil
		}
		if err != nil {
			return err
		}
		if !now.Before(verification.ExpiresAt) {
			verificationErr = ErrOTPExpired
			return nil
		}
		if verification.Attempts >= s.maxAttempts {
			verificationErr = ErrOTPAttemptsExceeded
			return nil
		}
		want := s.hashOTP(phone, otp)
		if subtle.ConstantTimeCompare([]byte(want), []byte(verification.OTPHash)) != 1 {
			if err := s.otps.IncrementAttempts(tx, verification.ID); err != nil {
				return err
			}
			verificationErr = ErrInvalidOTP
			return nil
		}
		if err := s.otps.Consume(tx, verification.ID, now); err != nil {
			return err
		}
		user, err = s.users.FindByPhone(tx, phone)
		if errors.Is(err, repository.ErrNotFound) {
			role := "customer"
			if phone == s.adminPhone {
				role = "admin"
			}
			user = &domain.User{ID: uuid.New(), Phone: phone, Role: role, Status: "active", PhoneVerifiedAt: &now, CreatedAt: now, UpdatedAt: now}
			if err := s.users.Create(tx, user); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if phone == s.adminPhone && user.Role != "admin" {
				user.Role = "admin"
			}
			user.PhoneVerifiedAt = &now
			user.UpdatedAt = now
			if err := s.users.Update(tx, user); err != nil {
				return err
			}
		}
		refreshToken, err = randomToken(32)
		if err != nil {
			return err
		}
		session = &domain.UserSession{ID: uuid.New(), UserID: user.ID, RefreshTokenHash: hashRefreshToken(refreshToken), ExpiresAt: now.Add(s.refreshTTL), CreatedAt: now}
		return s.sessions.Create(tx, session)
	})
	if err != nil {
		return dto.AuthResponse{}, err
	}
	if verificationErr != nil {
		return dto.AuthResponse{}, verificationErr
	}
	accessToken, err := s.token.Issue(user.ID, session.ID, now)
	if err != nil {
		return dto.AuthResponse{}, err
	}
	return dto.AuthResponse{User: userResponse(user), AccessToken: accessToken, RefreshToken: refreshToken, TokenType: "Bearer", ExpiresIn: int64(s.token.ttl.Seconds()), SessionID: session.ID.String()}, nil
}

func (s *AuthService) Refresh(ctx context.Context, rawRefreshToken string) (dto.AuthResponse, error) {
	if len(rawRefreshToken) < 32 {
		return dto.AuthResponse{}, ErrInvalidRefreshToken
	}
	now := time.Now().UTC()
	var user *domain.User
	var session *domain.UserSession
	var newRefreshToken string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		oldSession, err := s.sessions.FindByRefreshHashForUpdate(tx, hashRefreshToken(rawRefreshToken))
		if errors.Is(err, repository.ErrNotFound) {
			return ErrInvalidRefreshToken
		}
		if err != nil {
			return err
		}
		if oldSession.RevokedAt != nil || !now.Before(oldSession.ExpiresAt) {
			return ErrSessionRevoked
		}
		user, err = s.users.FindByID(tx, oldSession.UserID)
		if err != nil {
			return ErrInvalidRefreshToken
		}
		if err := s.sessions.Revoke(tx, oldSession.ID, now); err != nil {
			return err
		}
		newRefreshToken, err = randomToken(32)
		if err != nil {
			return err
		}
		session = &domain.UserSession{ID: uuid.New(), UserID: user.ID, RefreshTokenHash: hashRefreshToken(newRefreshToken), ExpiresAt: now.Add(s.refreshTTL), CreatedAt: now}
		return s.sessions.Create(tx, session)
	})
	if err != nil {
		return dto.AuthResponse{}, err
	}
	accessToken, err := s.token.Issue(user.ID, session.ID, now)
	if err != nil {
		return dto.AuthResponse{}, err
	}
	return dto.AuthResponse{User: userResponse(user), AccessToken: accessToken, RefreshToken: newRefreshToken, TokenType: "Bearer", ExpiresIn: int64(s.token.ttl.Seconds()), SessionID: session.ID.String()}, nil
}

func (s *AuthService) Logout(rawRefreshToken string) error {
	if rawRefreshToken == "" {
		return nil
	}
	session, err := s.sessions.FindByRefreshHashForUpdate(nil, hashRefreshToken(rawRefreshToken))
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.sessions.Revoke(nil, session.ID, time.Now().UTC())
}

func (s *AuthService) LogoutAll(userID uuid.UUID) error {
	return s.sessions.RevokeAll(nil, userID, time.Now().UTC())
}

func (s *AuthService) GetMe(userID uuid.UUID) (dto.UserResponseDto, error) {
	user, err := s.users.FindByID(nil, userID)
	if err != nil {
		return dto.UserResponseDto{}, err
	}
	return userResponse(user), nil
}

func (s *AuthService) UpdateProfile(userID uuid.UUID, name *string) (dto.UserResponseDto, error) {
	if name == nil {
		return s.GetMe(userID)
	}
	trimmed := strings.TrimSpace(*name)
	if len(trimmed) > 120 {
		return dto.UserResponseDto{}, fmt.Errorf("%w: name must be at most 120 characters", ErrInvalidInput)
	}
	user, err := s.users.FindByID(nil, userID)
	if err != nil {
		return dto.UserResponseDto{}, err
	}
	if trimmed == "" {
		user.Name = nil
	} else {
		user.Name = &trimmed
	}
	user.UpdatedAt = time.Now().UTC()
	if err := s.users.Update(nil, user); err != nil {
		return dto.UserResponseDto{}, err
	}
	return userResponse(user), nil
}

func (s *AuthService) DeleteAccount(userID uuid.UUID) error {
	// M-002: the open-order check and the anonymize must happen in the same
	// transaction so a new order cannot slip in between the two operations.
	return s.db.Transaction(func(tx *gorm.DB) error {
		// A customer with an open order, or a rider mid-delivery, can't vanish.
		var open int64
		if err := tx.Table("orders").Where(
			"(user_id = ? AND status IN ('pending','accepted')) OR (accepted_by = ? AND status = 'accepted')",
			userID, userID,
		).Count(&open).Error; err != nil {
			return err
		}
		if open > 0 {
			return ErrActiveOrders
		}
		if err := s.users.Anonymize(tx, userID); err != nil {
			return err
		}
		return s.sessions.RevokeAll(tx, userID, time.Now().UTC())
	})
}

func (s *AuthService) hashOTP(phone, otp string) string {
	mac := hmac.New(sha256.New, s.otpSecret)
	_, _ = mac.Write([]byte(phone + ":" + otp))
	return hex.EncodeToString(mac.Sum(nil))
}

func generateOTP() (string, error) {
	number, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", number.Int64()), nil
}

func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func userResponse(user *domain.User) dto.UserResponseDto {
	response := dto.UserResponseDto{ID: user.ID.String(), Phone: user.Phone, Name: user.Name, Status: user.Status, Role: user.Role}
	if user.PhoneVerifiedAt != nil {
		value := user.PhoneVerifiedAt.UTC().Format(time.RFC3339)
		response.PhoneVerifiedAt = &value
	}
	return response
}
