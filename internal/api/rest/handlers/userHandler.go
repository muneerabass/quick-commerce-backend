package handlers

import (
	"quick-commerce/internal/api/rest"
	"quick-commerce/internal/dto"
	"quick-commerce/internal/services"

	"github.com/gofiber/fiber/v2"
)

type UserHandler struct { 

	svc *services.UserService

}

func SetupUserRoutes(rh *rest.RestHandler) {

app := rh.App

h := UserHandler{

		svc: &services.UserService{},
	}
	
app.Get("/users", h.CreateUser)

}

func (h *UserHandler) GetAllUsers(c *fiber.Ctx) error {
	return c.SendString("Get all users")
}

func (h *UserHandler) CreateUser(c *fiber.Ctx) error {

	var userRequestDto dto.UserRequestDto

	if err := c.BodyParser(&userRequestDto); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	userResponseDto, err := h.svc.CreateUser(userRequestDto)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create user",
		})
	}

	return c.Status(fiber.StatusCreated).JSON(userResponseDto)

}