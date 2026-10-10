package configs

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type AppConfig struct {
	HTTPPort          string
	DatabaseURL       string
	JWTSecret         string
	OTPHashSecret     string
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	OTPTTL            time.Duration
	OTPResendAfter    time.Duration
	OTPMaxAttempts    int
	TwoFactorAPIKey   string
	TwoFactorTemplate string
	TwoFactorBaseURL  string
}

func LoadSettings() (AppConfig, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return AppConfig{}, fmt.Errorf("load .env: %w", err)
	}
	httpPort := os.Getenv("HTTP_PORT")
	databaseURL := os.Getenv("DATABASE_URL")

	if httpPort == "" {
		return AppConfig{}, fmt.Errorf("HTTP_PORT environment variable is not set")
	}
	if databaseURL == "" {
		return AppConfig{}, fmt.Errorf("DATABASE_URL environment variable is not set")
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if len(jwtSecret) < 32 {
		return AppConfig{}, fmt.Errorf("JWT_SECRET environment variable must be at least 32 characters")
	}
	otpHashSecret := os.Getenv("OTP_HASH_SECRET")
	if otpHashSecret == "" {
		otpHashSecret = jwtSecret
	}

	cfg := AppConfig{
		HTTPPort:          httpPort,
		DatabaseURL:       databaseURL,
		JWTSecret:         jwtSecret,
		OTPHashSecret:     otpHashSecret,
		AccessTokenTTL:    durationFromEnv("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:   durationFromEnv("REFRESH_TOKEN_TTL", 30*24*time.Hour),
		OTPTTL:            durationFromEnv("OTP_TTL", 5*time.Minute),
		OTPResendAfter:    durationFromEnv("OTP_RESEND_AFTER", 60*time.Second),
		OTPMaxAttempts:    intFromEnv("OTP_MAX_ATTEMPTS", 5),
		TwoFactorAPIKey:   os.Getenv("TWO_FACTOR_API_KEY"),
		TwoFactorTemplate: envOrDefault("TWO_FACTOR_TEMPLATE", "OTP1"),
		TwoFactorBaseURL:  envOrDefault("TWO_FACTOR_BASE_URL", "https://2factor.in"),
	}
	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}

func intFromEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
