package configs

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type AppConfig struct {
	// Order rules. Fees are in paise; see services.OrderPolicy.
	DeliveryFee           int
	HandlingFee           int
	FreeDeliveryThreshold int
	OrderAcceptTimeout    time.Duration
	StaleAcceptedAfter    time.Duration
	HTTPPort              string
	DatabaseURL           string
	JWTSecret             string
	OTPHashSecret         string
	AccessTokenTTL        time.Duration
	RefreshTokenTTL       time.Duration
	OTPTTL                time.Duration
	OTPResendAfter        time.Duration
	OTPMaxAttempts        int
	TwoFactorAPIKey       string
	TwoFactorTemplate     string
	TwoFactorBaseURL      string
	AdminPhone            string
	GoogleMapsAPIKey      string
	GoogleMapsBaseURL     string
	DevOTPLog             bool
	CloudinaryCloudName   string
	CloudinaryAPIKey      string
	CloudinaryAPISecret   string
}

func LoadSettings() (AppConfig, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return AppConfig{}, fmt.Errorf("load .env: %w", err)
	}
	// Cloud Run supplies PORT as a bare number (for example, "8080"), while
	// Fiber expects a listen address such as ":8080". HTTP_PORT is retained for
	// local development and takes precedence when it is explicitly configured.
	httpPort := os.Getenv("HTTP_PORT")
	if httpPort == "" {
		httpPort = os.Getenv("PORT")
	}
	if httpPort != "" && !strings.Contains(httpPort, ":") {
		httpPort = ":" + httpPort
	}
	databaseURL := os.Getenv("DATABASE_URL")

	if httpPort == "" {
		return AppConfig{}, fmt.Errorf("HTTP_PORT or PORT environment variable is not set")
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
		DeliveryFee:           nonNegativeIntFromEnv("DELIVERY_FEE", 2500),
		HandlingFee:           nonNegativeIntFromEnv("HANDLING_FEE", 400),
		FreeDeliveryThreshold: nonNegativeIntFromEnv("FREE_DELIVERY_THRESHOLD", 19900),
		OrderAcceptTimeout:    durationFromEnv("ORDER_ACCEPT_TIMEOUT", 30*time.Minute),
		StaleAcceptedAfter:    durationFromEnv("STALE_ACCEPTED_AFTER", 60*time.Minute),
		HTTPPort:              httpPort,
		DatabaseURL:           databaseURL,
		JWTSecret:             jwtSecret,
		OTPHashSecret:         otpHashSecret,
		AccessTokenTTL:        durationFromEnv("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:       durationFromEnv("REFRESH_TOKEN_TTL", 30*24*time.Hour),
		OTPTTL:                durationFromEnv("OTP_TTL", 5*time.Minute),
		OTPResendAfter:        durationFromEnv("OTP_RESEND_AFTER", 60*time.Second),
		OTPMaxAttempts:        intFromEnv("OTP_MAX_ATTEMPTS", 5),
		TwoFactorAPIKey:       os.Getenv("TWO_FACTOR_API_KEY"),
		TwoFactorTemplate:     envOrDefault("TWO_FACTOR_TEMPLATE", "OTP1"),
		TwoFactorBaseURL:      envOrDefault("TWO_FACTOR_BASE_URL", "https://2factor.in"),
		AdminPhone:            os.Getenv("ADMIN_PHONE"),
		GoogleMapsAPIKey:      envOrDefault("GOOGLE_MAPS_PLATFORM_KEY", os.Getenv("GOOGLE_MAPS_API_KEY")),
		GoogleMapsBaseURL:     envOrDefault("GOOGLE_MAPS_BASE_URL", "https://maps.googleapis.com"),
		DevOTPLog:             boolFromEnv("DEV_OTP_LOG", false),
		CloudinaryCloudName:   os.Getenv("CLOUDINARY_CLOUD_NAME"),
		CloudinaryAPIKey:      os.Getenv("CLOUDINARY_API_KEY"),
		CloudinaryAPISecret:   os.Getenv("CLOUDINARY_API_SECRET"),
	}
	return cfg, nil
}

func boolFromEnv(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch value {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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

// nonNegativeIntFromEnv is like intFromEnv but accepts 0 (e.g. "no delivery fee").
func nonNegativeIntFromEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
