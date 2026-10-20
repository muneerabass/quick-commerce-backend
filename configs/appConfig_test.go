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

	cfg, err := LoadSettings()
	if err != nil {
		t.Fatalf("unexpected error for valid production config: %v", err)
	}
	if cfg.DevOTPLog {
		t.Error("expected DevOTPLog=false for production config")
	}
}
