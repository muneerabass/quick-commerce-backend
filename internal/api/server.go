package api

import (
	"fmt"
	"quick-commerce/configs"
	"quick-commerce/internal/api/rest"
	"quick-commerce/internal/api/rest/handlers"
	"quick-commerce/internal/migrations"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func StartServer(config *configs.AppConfig) error {
	app := fiber.New()
	db, err := gorm.Open(postgres.Open(config.DatabaseURL), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to connect to the database: %w", err)
	}

	fmt.Println("Connected to the database successfully ")

	rh := &rest.RestHandler{
		App: app,
		Db:  db,
	}
	if err := migrations.Run(db); err != nil {
		return fmt.Errorf("failed to run database migrations: %w", err)
	}

	SetupRoutes(rh, *config)
	fmt.Println("Server is running on port", config.HTTPPort)
	return app.Listen(config.HTTPPort)
}

func SetupRoutes(rh *rest.RestHandler, config configs.AppConfig) {
	handlers.SetupUserRoutes(rh, config)
}
