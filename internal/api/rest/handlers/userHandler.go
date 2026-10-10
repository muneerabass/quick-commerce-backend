package handlers

import (
	"context"
	"errors"
	"quick-commerce/configs"
	"quick-commerce/internal/api/middleware"
	"quick-commerce/internal/api/rest"
	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"
	"quick-commerce/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type UserHandler struct {
	auth    *services.AuthService
	address *services.AddressService
}

func SetupUserRoutes(rh *rest.RestHandler, cfg configs.AppConfig) {
	users := repository.NewUserRepository(rh.Db)
	var sender services.OTPSender = services.UnconfiguredOTPSender{}
	if cfg.TwoFactorAPIKey != "" {
		sender = services.NewTwoFactorSender(cfg.TwoFactorBaseURL, cfg.TwoFactorAPIKey, cfg.TwoFactorTemplate)
	}
	auth := services.NewAuthService(rh.Db, users, repository.NewOTPRepository(rh.Db), repository.NewSessionRepository(rh.Db), sender, cfg.JWTSecret, cfg.OTPHashSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL, cfg.OTPTTL, cfg.OTPResendAfter, cfg.OTPMaxAttempts)
	if cfg.AdminPhone != "" {
		_ = auth.SetAdminPhone(cfg.AdminPhone)
	}
	h := UserHandler{auth: auth, address: services.NewAddressService(rh.Db, users, repository.NewAddressRepository(rh.Db))}

	api := rh.App.Group("/api/v1")
	authRoutes := api.Group("/auth")
	authRoutes.Post("/send-otp", h.SendOTP)
	authRoutes.Post("/verify-otp", h.VerifyOTP)
	authRoutes.Post("/refresh", h.Refresh)
	authRoutes.Post("/logout", h.Logout)
	authRoutes.Post("/logout-all", middleware.RequireAuth(authTokenService(h.auth), h.auth.ValidateAccessSession), h.LogoutAll)

	protected := api.Group("/users/me", middleware.RequireAuth(authTokenService(h.auth), h.auth.ValidateAccessSession))
	protected.Get("/", h.GetMe)
	protected.Patch("/", h.UpdateProfile)
	protected.Delete("/", h.DeleteAccount)
	protected.Post("/addresses", h.CreateAddress)
	protected.Get("/addresses", h.ListAddresses)
	protected.Get("/addresses/:addressID", h.GetAddress)
	protected.Patch("/addresses/:addressID", h.UpdateAddress)
	protected.Delete("/addresses/:addressID", h.DeleteAddress)
	protected.Put("/addresses/:addressID/default", h.SetDefaultAddress)

	SetupCatalogRoutes(rh, auth)
	SetupCommerceRoutes(rh, auth)
}

func authTokenService(auth *services.AuthService) *services.TokenService {
	return auth.TokenService()
}

func (h *UserHandler) SendOTP(c *fiber.Ctx) error {
	var request dto.SendOTPRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.auth.SendOTP(context.Background(), request.Phone); err != nil {
		return serviceError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"message": "OTP sent"})
}

func (h *UserHandler) VerifyOTP(c *fiber.Ctx) error {
	var request dto.VerifyOTPRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	response, err := h.auth.VerifyOTP(context.Background(), request.Phone, request.OTP)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(response)
}

func (h *UserHandler) Refresh(c *fiber.Ctx) error {
	var request dto.RefreshTokenRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	response, err := h.auth.Refresh(context.Background(), request.RefreshToken)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(response)
}

func (h *UserHandler) Logout(c *fiber.Ctx) error {
	var request dto.RefreshTokenRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.auth.Logout(request.RefreshToken); err != nil {
		return serviceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandler) LogoutAll(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	if err := h.auth.LogoutAll(userID); err != nil {
		return serviceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandler) GetMe(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	response, err := h.auth.GetMe(userID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(response)
}

func (h *UserHandler) UpdateProfile(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	var request dto.UpdateProfileRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	response, err := h.auth.UpdateProfile(userID, request.Name)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(response)
}

func (h *UserHandler) DeleteAccount(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	if err := h.auth.DeleteAccount(userID); err != nil {
		return serviceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandler) ListAddresses(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	addresses, err := h.address.List(userID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(fiber.Map{"addresses": addresses})
}

func (h *UserHandler) GetAddress(c *fiber.Ctx) error {
	userID, addressID, err := requestIDs(c)
	if err != nil {
		return serviceError(c, err)
	}
	address, err := h.address.Get(userID, addressID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(address)
}

func (h *UserHandler) CreateAddress(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	var request dto.AddressRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	address, err := h.address.Create(userID, request)
	if err != nil {
		return serviceError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(address)
}

func (h *UserHandler) UpdateAddress(c *fiber.Ctx) error {
	userID, addressID, err := requestIDs(c)
	if err != nil {
		return serviceError(c, err)
	}
	var request dto.AddressPatchRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	address, err := h.address.Update(userID, addressID, request)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(address)
}

func (h *UserHandler) DeleteAddress(c *fiber.Ctx) error {
	userID, addressID, err := requestIDs(c)
	if err != nil {
		return serviceError(c, err)
	}
	if err := h.address.Delete(userID, addressID); err != nil {
		return serviceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandler) SetDefaultAddress(c *fiber.Ctx) error {
	userID, addressID, err := requestIDs(c)
	if err != nil {
		return serviceError(c, err)
	}
	address, err := h.address.SetDefault(userID, addressID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(address)
}

func requestIDs(c *fiber.Ctx) (uuid.UUID, uuid.UUID, error) {
	userID, ok := middleware.UserID(c)
	if !ok {
		return uuid.Nil, uuid.Nil, errors.New("authentication required")
	}
	addressID, err := uuid.Parse(c.Params("addressID"))
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("invalid address id")
	}
	return userID, addressID, nil
}

func serviceError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, services.ErrInvalidInput), errors.Is(err, services.ErrInvalidPhone), errors.Is(err, services.ErrInvalidOTP), errors.Is(err, services.ErrOTPExpired), errors.Is(err, services.ErrOTPAttemptsExceeded), errors.Is(err, services.ErrInvalidRefreshToken), errors.Is(err, services.ErrSessionRevoked), errors.Is(err, services.ErrCartStoreMismatch), errors.Is(err, services.ErrEmptyCart), errors.Is(err, services.ErrInsufficientStock), errors.Is(err, services.ErrInvalidRole), errors.Is(err, repository.ErrNotFound):
		return writeError(c, fiber.StatusBadRequest, err.Error())
	case errors.Is(err, repository.ErrOrderUnavailable), errors.Is(err, gorm.ErrDuplicatedKey):
		return writeError(c, fiber.StatusConflict, err.Error())
	case errors.Is(err, services.ErrOTPTooSoon), errors.Is(err, services.ErrOTPRateLimited):
		return writeError(c, fiber.StatusTooManyRequests, err.Error())
	case errors.Is(err, services.ErrOTPProvider):
		return writeError(c, fiber.StatusServiceUnavailable, "OTP provider is not configured")
	case errors.Is(err, gorm.ErrRecordNotFound):
		return writeError(c, fiber.StatusNotFound, "resource not found")
	case errors.Is(err, services.ErrAccountDeleted):
		return writeError(c, fiber.StatusGone, err.Error())
	default:
		return writeError(c, fiber.StatusInternalServerError, "internal server error")
	}
}

func writeError(c *fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(fiber.Map{"error": message})
}
