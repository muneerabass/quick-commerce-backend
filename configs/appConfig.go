package configs

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type AppConfig struct {
	HTTPPort    string
	DatabaseURL string
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

	cfg := AppConfig{
		HTTPPort:    httpPort,
		DatabaseURL: databaseURL,
	}
	return cfg, nil
}
