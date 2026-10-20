package handlers

import (
	"net/http/httptest"
	"testing"
	"time"

	"quick-commerce/configs"
	"quick-commerce/internal/api/rest"
	"quick-commerce/internal/domain"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testRouteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open("file:route_test_"+uuid.NewString()+"?mode=memory&cache=shared"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.OTPVerification{},
		&domain.UserSession{},
		&domain.UserAddress{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func minimalCfg(devOTPLog bool) configs.AppConfig {
	return configs.AppConfig{
		DevOTPLog:       devOTPLog,
		JWTSecret:       "test-jwt-secret-must-be-at-least-32-chars!!",
		OTPHashSecret:   "test-otp-hash-secret-32-chars-min",
		AccessTokenTTL:  time.Minute,
		RefreshTokenTTL: 24 * time.Hour,
		OTPTTL:          5 * time.Minute,
		OTPResendAfter:  60 * time.Second,
		OTPMaxAttempts:  5,
	}
}

// TestDevOTPRouteAbsentWhenDisabled verifies that the dev OTP route is not
// registered in Fiber's route stack when DevOTPLog is false.
//
// NOTE: a plain HTTP check cannot always distinguish "route not registered"
// from "middleware returned early" — commerceHandler registers a USE-level
// authenticated middleware at /api/v1/ that short-circuits ALL unmatched paths
// under that prefix with a 401. We therefore check the route stack directly,
// which is the only reliable way to assert "the route was never registered".
func TestDevOTPRouteAbsentWhenDisabled(t *testing.T) {
	db := testRouteDB(t)
	app := fiber.New(fiber.Config{ErrorHandler: JSONErrorHandler})

	SetupUserRoutes(&rest.RestHandler{App: app, Db: db}, minimalCfg(false))

	const devOTPPath = "/api/v1/auth/dev/last-otp"
	for _, routes := range app.Stack() {
		for _, route := range routes {
			if route.Path == devOTPPath {
				t.Errorf("dev OTP route %s must not be registered when DevOTPLog=false", devOTPPath)
			}
		}
	}
}

// TestDevOTPRouteRefusesProxiedRequests verifies that even when DevOTPLog is
// true the handler rejects requests that arrive through a proxy or tunnel
// (identified by X-Forwarded-For or X-Forwarded-Host headers).
func TestDevOTPRouteRefusesProxiedRequests(t *testing.T) {
	db := testRouteDB(t)
	app := fiber.New(fiber.Config{ErrorHandler: JSONErrorHandler})

	SetupUserRoutes(&rest.RestHandler{App: app, Db: db}, minimalCfg(true))

	for _, tc := range []struct {
		header string
		value  string
	}{
		{"X-Forwarded-For", "1.2.3.4"},
		{"X-Forwarded-Host", "0be0.ngrok-free.app"},
	} {
		req := httptest.NewRequest("GET", "/api/v1/auth/dev/last-otp?phone=%2B919999999999", nil)
		req.Header.Set(tc.header, tc.value)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusNotFound {
			t.Errorf("%s header: expected 404, got %d", tc.header, resp.StatusCode)
		}
	}
}
