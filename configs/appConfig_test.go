package configs

import (
	"testing"
)

// minimalEnv sets only the env vars that LoadSettings requires, leaving all
// others at their zero value so tests can override individual keys cleanly.
func minimalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HTTP_PORT", ":3099")
	t.Setenv("DATABASE_URL", "postgresql://test:test@localhost/testdb")
	t.Setenv("JWT_SECRET", "test-jwt-secret-must-be-at-least-32-chars!!")
}

func TestLoadSettingsRejectsProductionWithDevOTPLog(t *testing.T) {
	minimalEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DEV_OTP_LOG", "true")

	_, err := LoadSettings()
	if err == nil {
		t.Fatal("expected error when APP_ENV=production and DEV_OTP_LOG=true, got nil")
	}
}

func TestLoadSettingsAllowsDevOTPLogOutsideProduction(t *testing.T) {
	minimalEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("DEV_OTP_LOG", "true")

	cfg, err := LoadSettings()
	if err != nil {
		t.Fatalf("unexpected error with APP_ENV=development and DEV_OTP_LOG=true: %v", err)
	}
	if !cfg.DevOTPLog {
		t.Error("expected DevOTPLog=true")
	}
}

func TestLoadSettingsAllowsProductionWithoutDevOTPLog(t *testing.T) {
	minimalEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DEV_OTP_LOG", "false")
	// M-004/M-007: production requires distinct OTP_HASH_SECRET and ALLOWED_ORIGINS.
	t.Setenv("OTP_HASH_SECRET", "different-otp-hash-secret-at-least-32-chars!")
	t.Setenv("ALLOWED_ORIGINS", "https://app.example.com")

	cfg, err := LoadSettings()
	if err != nil {
		t.Fatalf("unexpected error for valid production config: %v", err)
	}
	if cfg.DevOTPLog {
		t.Error("expected DevOTPLog=false for production config")
	}
}

// TestLoadSettingsRequiresDistinctOTPSecretInProduction verifies M-004.
func TestLoadSettingsRequiresDistinctOTPSecretInProduction(t *testing.T) {
	minimalEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("ALLOWED_ORIGINS", "https://app.example.com")
	// OTP_HASH_SECRET missing → must fail.
	if _, err := LoadSettings(); err == nil {
		t.Fatal("expected error when OTP_HASH_SECRET is missing in production")
	}
	// OTP_HASH_SECRET same as JWT_SECRET → must fail.
	t.Setenv("OTP_HASH_SECRET", "test-jwt-secret-must-be-at-least-32-chars!!")
	if _, err := LoadSettings(); err == nil {
		t.Fatal("expected error when OTP_HASH_SECRET equals JWT_SECRET in production")
	}
}

// TestLoadSettingsRequiresAllowedOriginsInProduction verifies M-007.
func TestLoadSettingsRequiresAllowedOriginsInProduction(t *testing.T) {
	minimalEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("OTP_HASH_SECRET", "different-otp-hash-secret-at-least-32-chars!")
	// ALLOWED_ORIGINS missing → must fail.
	if _, err := LoadSettings(); err == nil {
		t.Fatal("expected error when ALLOWED_ORIGINS is missing in production")
	}
}

// TestLoadSettingsDevAllowsNoAllowedOrigins verifies that development does not
// require ALLOWED_ORIGINS (it falls back to "*" in server.go) (M-007).
func TestLoadSettingsDevAllowsNoAllowedOrigins(t *testing.T) {
	minimalEnv(t)
	t.Setenv("APP_ENV", "development")
	if _, err := LoadSettings(); err != nil {
		t.Fatalf("unexpected error in development without ALLOWED_ORIGINS: %v", err)
	}
}
