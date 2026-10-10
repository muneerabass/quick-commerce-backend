package middleware

import (
	"quick-commerce/internal/services"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const (
	UserIDLocal    = "authenticated_user_id"
	SessionIDLocal = "authenticated_session_id"
)

func RequireAuth(tokens *services.TokenService, validateSession func(uuid.UUID, uuid.UUID) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get(fiber.HeaderAuthorization)
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "authentication required"})
		}
		claims, err := tokens.Parse(parts[1], time.Now().UTC())
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid access token"})
		}
		if validateSession != nil {
			if err := validateSession(claims.UserID, claims.SessionID); err != nil {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "session is no longer active"})
			}
		}
		c.Locals(UserIDLocal, claims.UserID)
		c.Locals(SessionIDLocal, claims.SessionID)
		return c.Next()
	}
}

func UserID(c *fiber.Ctx) (uuid.UUID, bool) {
	id, ok := c.Locals(UserIDLocal).(uuid.UUID)
	return id, ok && id != uuid.Nil
}

func RequireRole(lookup func(uuid.UUID) (string, error), allowed ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, ok := UserID(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "authentication required"})
		}
		role, err := lookup(userID)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "authentication required"})
		}
		for _, candidate := range allowed {
			if role == candidate {
				return c.Next()
			}
		}
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "insufficient permissions"})
	}
}
