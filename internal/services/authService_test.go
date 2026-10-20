package services

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"quick-commerce/internal/domain"
	"quick-commerce/internal/dto"
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

	if err := auth.SendOTP(context.Background(), phone, ""); err != nil {
		t.Fatal(err)
	}
	if len(sender.otp) != 6 {
		t.Fatalf("expected six-digit OTP, got %q", sender.otp)
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

	if err := auth.SendOTP(context.Background(), phone, ""); err != nil {
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
	if err := auth.SendOTP(context.Background(), "+919876543211", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.VerifyOTP(context.Background(), "+919876543211", sender.otp); !errors.Is(err, ErrOTPExpired) {
		t.Fatalf("expected expiry error, got %v", err)
	}

	db2 := testDB(t)
	sender2 := &usefulRecordingSender{}
	auth2 := testAuthService(db2, sender2, 5*time.Minute, 0, 2)
	phone := "+919876543212"
	if err := auth2.SendOTP(context.Background(), phone, ""); err != nil {
		t.Fatal(err)
	}
	wrongOTP := "000000"
	if wrongOTP == sender2.otp {
		wrongOTP = "111111"
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
	if err := auth.SendOTP(context.Background(), phone, ""); err != nil {
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
	if err := auth.SendOTP(context.Background(), phone, ""); err != nil {
		t.Fatal(err)
	}
	if err := auth.SendOTP(context.Background(), phone, ""); !errors.Is(err, ErrOTPTooSoon) {
		t.Fatalf("expected resend limit error, got %v", err)
	}

	db2 := testDB(t)
	sender2 := &usefulRecordingSender{}
	auth2 := testAuthService(db2, sender2, 5*time.Minute, 0, 5)
	for i := 0; i < 5; i++ {
		if err := auth2.SendOTP(context.Background(), "+919876543215", ""); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	if err := auth2.SendOTP(context.Background(), "+919876543215", ""); !errors.Is(err, ErrOTPRateLimited) {
		t.Fatalf("expected hourly rate limit error, got %v", err)
	}
}

// --- Phase 3 regression tests ---

// TestTokenServiceHS256OnlyRejectsAlgNone verifies that the library-backed
// parser refuses tokens with alg:none regardless of whether the signature
// field is empty (H-001).
func TestTokenServiceHS256OnlyRejectsAlgNone(t *testing.T) {
	ts := NewTokenService("super-secret-key-at-least-32-bytes!!", time.Minute)
	now := time.Now().UTC()

	// Issue a legitimate token and verify it parses correctly.
	token, err := ts.Issue(uuid.New(), uuid.New(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Parse(token, now); err != nil {
		t.Fatalf("valid token should parse: %v", err)
	}

	// Craft an alg:none token by replacing the header segment.
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		t.Fatal("expected 3 JWT parts")
	}
	algNoneToken := noneHeader + "." + parts[1] + "."
	if _, err := ts.Parse(algNoneToken, now); err == nil {
		t.Fatal("alg:none token should be rejected")
	}
}

// TestTokenServiceRejectsTamperedPayload verifies signature verification (H-001).
func TestTokenServiceRejectsTamperedPayload(t *testing.T) {
	ts := NewTokenService("super-secret-key-at-least-32-bytes!!", time.Minute)
	now := time.Now().UTC()
	token, err := ts.Issue(uuid.New(), uuid.New(), now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(token, ".", 3)
	// Flip a character in the payload to simulate tampering.
	tampered := parts[0] + "." + parts[1] + "X" + "." + parts[2]
	if _, err := ts.Parse(tampered, now); err == nil {
		t.Fatal("tampered token should be rejected")
	}
}

// TestTokenServiceRejectsWrongKey verifies that a token signed with a different
// key is rejected (H-001).
func TestTokenServiceRejectsWrongKey(t *testing.T) {
	ts := NewTokenService("super-secret-key-at-least-32-bytes!!", time.Minute)
	ts2 := NewTokenService("different-secret-key-at-least-32-b!!", time.Minute)
	now := time.Now().UTC()
	token, err := ts.Issue(uuid.New(), uuid.New(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts2.Parse(token, now); err == nil {
		t.Fatal("token signed by different key should be rejected")
	}
}

// TestTokenServiceRejectsExpiredToken verifies that expired tokens are rejected
// (H-001).
func TestTokenServiceRejectsExpiredToken(t *testing.T) {
	ts := NewTokenService("super-secret-key-at-least-32-bytes!!", time.Millisecond)
	past := time.Now().UTC().Add(-time.Hour)
	token, err := ts.Issue(uuid.New(), uuid.New(), past)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Parse(token, time.Now().UTC()); err == nil {
		t.Fatal("expired token should be rejected")
	}
}

// TestTokenServiceRejectsMalformedTokens verifies that malformed input is
// handled gracefully (H-001).
func TestTokenServiceRejectsMalformedTokens(t *testing.T) {
	ts := NewTokenService("super-secret-key-at-least-32-bytes!!", time.Minute)
	now := time.Now().UTC()
	for _, bad := range []string{"", "x", "x.y", "x.y.z", "..", "a.b.c.d"} {
		if _, err := ts.Parse(bad, now); err == nil {
			t.Fatalf("expected error for malformed token %q", bad)
		}
	}
}

// TestOTPDailyPhoneLimit verifies the 15/day per-phone cap (M-005).
func TestOTPDailyPhoneLimit(t *testing.T) {
	db := testDB(t)
	sender := &usefulRecordingSender{}
	auth := testAuthService(db, sender, 5*time.Minute, 0, 5)
	phone := "+919876540099"

	// Insert 15 OTP records directly so the daily cap is reached.
	for i := 0; i < otpDailyPhoneLimit; i++ {
		otp := domain.OTPVerification{
			ID:        uuid.New(),
			Phone:     phone,
			OTPHash:   "x",
			ExpiresAt: time.Now().UTC().Add(time.Minute),
			CreatedAt: time.Now().UTC(),
		}
		if err := db.Create(&otp).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := auth.SendOTP(context.Background(), phone, ""); !errors.Is(err, ErrOTPRateLimited) {
		t.Fatalf("expected daily limit error, got %v", err)
	}
}

// TestOTPIPHourlyLimit verifies that the per-IP rate limit fires after
// otpHourlyIPLimit sends from the same address (M-005).
// NOTE: this is a per-instance in-memory counter; see ipBucket note.
func TestOTPIPHourlyLimit(t *testing.T) {
	db := testDB(t)
	sender := &usefulRecordingSender{}
	auth := testAuthService(db, sender, 5*time.Minute, 0, 5)

	ip := "10.0.0.1"
	for i := 0; i < otpHourlyIPLimit; i++ {
		// Use distinct phones to avoid the per-phone hourly cap.
		phone := "+91987654" + fourDigit(i)
		if err := auth.SendOTP(context.Background(), phone, ip); err != nil {
			t.Fatalf("send %d from ip: %v", i+1, err)
		}
	}
	// One more from the same IP should be rejected.
	if err := auth.SendOTP(context.Background(), "+919876540000", ip); !errors.Is(err, ErrOTPRateLimited) {
		t.Fatalf("expected IP rate limit error, got %v", err)
	}
	// A different IP is unaffected.
	if err := auth.SendOTP(context.Background(), "+919876540000", "10.0.0.2"); err != nil {
		t.Fatalf("different IP should not be rate-limited: %v", err)
	}
}

// TestAddressLimitReached verifies that a 21st address is rejected (M-006).
func TestAddressLimitReached(t *testing.T) {
	db := testDB(t)
	users := repository.NewUserRepository(db)
	addrs := NewAddressService(db, users, repository.NewAddressRepository(db))
	userID := uuid.New()
	name := "Limiter"
	if err := db.Create(&domain.User{ID: userID, Phone: "+919876540088", Name: &name, Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	req := dto.AddressRequest{
		RecipientName: "R", Phone: "+919876540088",
		AddressLine1: "1 St", City: "C", State: "S", Pincode: "400001",
	}
	for i := 0; i < maxAddressesPerUser; i++ {
		req.Label = labelForIndex(i)
		if _, err := addrs.Create(userID, req); err != nil {
			t.Fatalf("address %d of %d: %v", i+1, maxAddressesPerUser, err)
		}
	}
	req.Label = "Other"
	if _, err := addrs.Create(userID, req); !errors.Is(err, ErrAddressLimitReached) {
		t.Fatalf("expected address limit error, got %v", err)
	}
}

// TestDeliveryCodeHashing verifies that generateDeliveryCode produces 6 digits,
// hashDeliveryCode produces a stable hmac: prefix, and verification works for
// both hashed and legacy plaintext codes (L-004/L-006).
func TestDeliveryCodeHashing(t *testing.T) {
	code, err := generateDeliveryCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Fatalf("expected 6-digit code, got %q (len=%d)", code, len(code))
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			t.Fatalf("expected digits only, got %q", code)
		}
	}

	secret := []byte("test-delivery-secret")
	orderID := uuid.New()
	hash := hashDeliveryCode(secret, orderID, code)
	if !isHashedDeliveryCode(hash) {
		t.Fatalf("hashDeliveryCode should produce hmac: prefix, got %q", hash)
	}
	// Same inputs produce the same hash (deterministic).
	if hash2 := hashDeliveryCode(secret, orderID, code); hash != hash2 {
		t.Fatal("hashDeliveryCode is not deterministic")
	}
	// Different code produces different hash.
	different := "000000"
	if code == different {
		different = "000001"
	}
	if hashDeliveryCode(secret, orderID, different) == hash {
		t.Fatal("different codes must produce different hashes")
	}
}

// TestDeliveryCodeLegacyPlaintextPassthrough verifies that legacy 4-digit
// plaintext codes are still verified correctly after the migration (L-004/L-006).
func TestDeliveryCodeLegacyPlaintextPassthrough(t *testing.T) {
	db := testDB(t)
	if err := db.AutoMigrate(
		&domain.Order{}, &domain.OrderEvent{}, &domain.Cart{}, &domain.CartItem{},
		&domain.Product{}, &domain.Store{}, &domain.Inventory{},
		&domain.OrderItem{}, &domain.StorePincode{}, &domain.DeliveryStoreAssignment{},
	); err != nil {
		t.Fatal(err)
	}
	commerce := repository.NewCommerceRepository(db)
	orderID := uuid.New()
	userID := uuid.New()
	storeID := uuid.New()
	legacyCode := "1234"
	if err := db.Create(&domain.Order{
		ID: orderID, UserID: userID, StoreID: storeID, Status: "accepted",
		DeliveryCode: legacyCode, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	orders := NewOrderService(db, repository.NewAddressRepository(db), repository.NewCatalogRepository(db), commerce).
		WithDeliveryCodeSecret("test-secret")

	order, err := commerce.FindOrderByID(nil, orderID)
	if err != nil {
		t.Fatal(err)
	}
	// Correct legacy code passes.
	if err := orders.verifyDeliveryCode(order, legacyCode); err != nil {
		t.Fatalf("legacy plaintext code should verify: %v", err)
	}
	// Wrong code fails.
	if err := orders.verifyDeliveryCode(order, "9999"); err == nil {
		t.Fatal("wrong legacy code should be rejected")
	}
}

// --- test helpers ---

func fourDigit(n int) string {
	s := strings.Repeat("0", 4) + itoa(n)
	return s[len(s)-4:]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func labelForIndex(i int) string {
	switch i % 3 {
	case 0:
		return "Home"
	case 1:
		return "Work"
	default:
		return "Other"
	}
}
