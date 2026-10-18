package api

import (
	"fmt"
	"quick-commerce/configs"
	"quick-commerce/internal/api/rest"
	"quick-commerce/internal/api/rest/handlers"
	"quick-commerce/internal/migrations"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func StartServer(config *configs.AppConfig) error {
	app := fiber.New(fiber.Config{ErrorHandler: handlers.JSONErrorHandler, BodyLimit: 2 * 1024 * 1024})
	app.Use(recover.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin,Content-Type,Accept,Authorization",
		AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS",
	}))
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
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "ok"})
	})

	SetupRoutes(rh, *config)
	// Registered last so unknown routes get a JSON 404 instead of plain text.
	app.Use(func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "route not found", "code": "not_found"})
	})
	fmt.Println("Server is running on port", config.HTTPPort)
	return app.Listen(config.HTTPPort)
}

func SetupRoutes(rh *rest.RestHandler, config configs.AppConfig) {
	handlers.SetupUserRoutes(rh, config)
}
