package api

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"quick-commerce/configs"
	"quick-commerce/internal/api/rest"
	"quick-commerce/internal/api/rest/handlers"
	"quick-commerce/internal/migrations"
	"quick-commerce/internal/repository"
	"quick-commerce/internal/services"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func StartServer(config *configs.AppConfig) error {
	app := fiber.New(fiber.Config{ErrorHandler: handlers.JSONErrorHandler, BodyLimit: 2 * 1024 * 1024})
	app.Use(recover.New())
	// M-007: Use a configurable origin allowlist. In production ALLOWED_ORIGINS
	// must be set (enforced in LoadSettings). In development the empty value
	// falls back to "*" so the local dev server works without extra config.
	corsOrigins := config.AllowedOrigins
	if corsOrigins == "" {
		corsOrigins = "*"
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins: corsOrigins,
		AllowHeaders: "Origin,Content-Type,Accept,Authorization",
		AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS",
	}))
	// M-009: Request logging. ${path} does not include query parameters,
	// avoiding accidental logging of phone numbers or other sensitive values.
	// Authorization headers and request bodies are never included.
	app.Use(logger.New(logger.Config{
		Format:     "${time} | ${status} | ${latency} | ${method} | ${path}\n",
		TimeFormat: "2006-01-02T15:04:05Z07:00",
		TimeZone:   "UTC",
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
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "ok"})
	})

	services.SetOrderPolicy(services.OrderPolicy{
		DeliveryFee: config.DeliveryFee, HandlingFee: config.HandlingFee, FreeDeliveryThreshold: config.FreeDeliveryThreshold,
		AcceptTimeout: config.OrderAcceptTimeout, StaleAcceptedAfter: config.StaleAcceptedAfter,
	})
	sweeperCtx, stopSweeper := context.WithCancel(context.Background())
	services.StartOrderSweeper(sweeperCtx, services.NewOrderService(db, repository.NewAddressRepository(db), repository.NewCatalogRepository(db), repository.NewCommerceRepository(db)), time.Minute)

	SetupRoutes(rh, *config)
	// Registered last so unknown routes get a JSON 404 instead of plain text.
	app.Use(func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "route not found", "code": "not_found"})
	})

	// H-002: Graceful shutdown. Listen for SIGTERM (container scheduler) and
	// SIGINT (Ctrl+C / local dev). Stop accepting new requests and let in-flight
	// requests finish within the shutdown window before the process exits.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		fmt.Println("Server is running on port", config.HTTPPort)
		serverErr <- app.Listen(config.HTTPPort)
	}()

	select {
	case err := <-serverErr:
		stopSweeper()
		return err
	case sig := <-quit:
		log.Printf("received signal %s, shutting down gracefully", sig)
		stopSweeper()
		if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
			return fmt.Errorf("graceful shutdown timed out: %w", err)
		}
		return nil
	}
}

func SetupRoutes(rh *rest.RestHandler, config configs.AppConfig) {
	handlers.SetupUserRoutes(rh, config)
}
