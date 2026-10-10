package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"quick-commerce/internal/domain"
	"quick-commerce/internal/repository"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type usefulRecordingSender struct{ otp string }

func (s *usefulRecordingSender) Send(_ context.Context, _, otp string) error {
	s.otp = otp
	return nil
}

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:test_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.OTPVerification{}, &domain.UserSession{}, &domain.UserAddress{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func testAuthService(db *gorm.DB, sender OTPSender, otpTTL, resendAfter time.Duration, maxAttempts int) *AuthService {
	users := repository.NewUserRepository(db)
	return NewAuthService(db, users, repository.NewOTPRepository(db), repository.NewSessionRepository(db), sender, "test-jwt-secret", "test-otp-secret", time.Minute, 24*time.Hour, otpTTL, resendAfter, maxAttempts)
}

func TestAuthServiceOTPFirstLoginAndReusePrevention(t *testing.T) {
	db := testDB(t)
	sender := &usefulRecordingSender{}
	auth := testAuthService(db, sender, 5*time.Minute, 0, 5)
	phone := "+919876543210"

	if err := auth.SendOTP(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	if len(sender.otp) != 4 {
		t.Fatalf("expected four-digit OTP, got %q", sender.otp)
	}
	response, err := auth.VerifyOTP(context.Background(), phone, sender.otp)
	if err != nil {
		t.Fatal(err)
	}
	if response.User.ID == "" || response.User.Phone != phone || response.RefreshToken == "" {
		t.Fatalf("unexpected auth response: %+v", response)
	}
	var count int64
	if err := db.Model(&domain.User{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected one created user, count=%d, err=%v", count, err)
	}
	if _, err := auth.VerifyOTP(context.Background(), phone, sender.otp); !errors.Is(err, ErrInvalidOTP) {
		t.Fatalf("expected consumed OTP to be rejected, got %v", err)
	}

	if err := auth.SendOTP(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	second, err := auth.VerifyOTP(context.Background(), phone, sender.otp)
	if err != nil {
		t.Fatal(err)
	}
	if second.User.ID != response.User.ID {
		t.Fatalf("expected existing user to be reused: first=%s second=%s", response.User.ID, second.User.ID)
	}
	name := "Updated Name"
	updated, err := auth.UpdateProfile(uuid.MustParse(response.User.ID), &name)
	if err != nil || updated.Name == nil || *updated.Name != name {
		t.Fatalf("expected profile update, response=%+v err=%v", updated, err)
	}
}

func TestAuthServiceOTPExpiryAndAttemptLimit(t *testing.T) {
	db := testDB(t)
	sender := &usefulRecordingSender{}
	auth := testAuthService(db, sender, -time.Second, 0, 2)
	if err := auth.SendOTP(context.Background(), "+919876543211"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.VerifyOTP(context.Background(), "+919876543211", sender.otp); !errors.Is(err, ErrOTPExpired) {
		t.Fatalf("expected expiry error, got %v", err)
	}

	db2 := testDB(t)
	sender2 := &usefulRecordingSender{}
	auth2 := testAuthService(db2, sender2, 5*time.Minute, 0, 2)
	phone := "+919876543212"
	if err := auth2.SendOTP(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	wrongOTP := "0000"
	if wrongOTP == sender2.otp {
		wrongOTP = "1111"
	}
	for i := 0; i < 2; i++ {
		if _, err := auth2.VerifyOTP(context.Background(), phone, wrongOTP); !errors.Is(err, ErrInvalidOTP) {
			t.Fatalf("wrong attempt %d: expected invalid OTP, got %v", i+1, err)
		}
	}
	if _, err := auth2.VerifyOTP(context.Background(), phone, sender2.otp); !errors.Is(err, ErrOTPAttemptsExceeded) {
		t.Fatalf("expected attempt limit error, got %v", err)
	}
}

func TestAuthServiceRefreshRotationAndRevocation(t *testing.T) {
	db := testDB(t)
	sender := &usefulRecordingSender{}
	auth := testAuthService(db, sender, 5*time.Minute, 0, 5)
	phone := "+919876543213"
	if err := auth.SendOTP(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	first, err := auth.VerifyOTP(context.Background(), phone, sender.otp)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := auth.Refresh(context.Background(), first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.RefreshToken == first.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}
	if _, err := auth.Refresh(context.Background(), first.RefreshToken); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("expected old refresh token rejection, got %v", err)
	}
	if err := auth.Logout(rotated.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Refresh(context.Background(), rotated.RefreshToken); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("expected revoked refresh token rejection, got %v", err)
	}
	if _, err := auth.GetMe(uuid.MustParse(rotated.User.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestAuthServiceResendAndRateLimits(t *testing.T) {
	db := testDB(t)
	sender := &usefulRecordingSender{}
	auth := testAuthService(db, sender, 5*time.Minute, time.Hour, 5)
	phone := "+919876543214"
	if err := auth.SendOTP(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	if err := auth.SendOTP(context.Background(), phone); !errors.Is(err, ErrOTPTooSoon) {
		t.Fatalf("expected resend limit error, got %v", err)
	}

	db2 := testDB(t)
	sender2 := &usefulRecordingSender{}
	auth2 := testAuthService(db2, sender2, 5*time.Minute, 0, 5)
	for i := 0; i < 5; i++ {
		if err := auth2.SendOTP(context.Background(), "+919876543215"); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	if err := auth2.SendOTP(context.Background(), "+919876543215"); !errors.Is(err, ErrOTPRateLimited) {
		t.Fatalf("expected hourly rate limit error, got %v", err)
	}
}
