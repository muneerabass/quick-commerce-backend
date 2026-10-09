package api

import (
	"fmt"
	"quick-commerce/configs"
	"quick-commerce/internal/api/rest"
	"quick-commerce/internal/api/rest/handlers"

	"github.com/gofiber/fiber/v2"
)




func StartServer(config *configs.AppConfig) error {
	app := fiber.New()
	
	rh := &rest.RestHandler{App: app}
	SetupRoutes(rh)

	fmt.Println("Server is running on port", config.HTTPPort)
	return app.Listen(config.HTTPPort)
}

func SetupRoutes(rh *rest.RestHandler) {
	handlers.SetupUserRoutes(rh)
}