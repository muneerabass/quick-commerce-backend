package handlers

import (
	"context"
	"errors"
	"log"
	"quick-commerce/configs"
	"quick-commerce/internal/api/middleware"
	"quick-commerce/internal/api/rest"
	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"
	"quick-commerce/internal/services"
	"strings"

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
	// Dev-mode swap: when DEV_OTP_LOG is on, OTP delivery is a noop stdout log so
	// a developer can log in without a real SMS provider.
	if cfg.DevOTPLog {
		sender = services.DevStdoutOTPSender{}
	}
	auth := services.NewAuthService(rh.Db, users, repository.NewOTPRepository(rh.Db), repository.NewSessionRepository(rh.Db), sender, cfg.JWTSecret, cfg.OTPHashSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL, cfg.OTPTTL, cfg.OTPResendAfter, cfg.OTPMaxAttempts)
	auth.SetDevOTPLog(cfg.DevOTPLog)
	if cfg.AdminPhone != "" {
		_ = auth.SetAdminPhone(cfg.AdminPhone)
	}
	addressRepository := repository.NewAddressRepository(rh.Db)
	addressService := services.NewAddressService(rh.Db, users, addressRepository)
	if cfg.GoogleMapsAPIKey != "" {
		addressService = services.NewAddressServiceWithGeocoder(rh.Db, users, addressRepository, services.NewGoogleMapsGeocoder(cfg.GoogleMapsBaseURL, cfg.GoogleMapsAPIKey))
	}
	h := UserHandler{auth: auth, address: addressService}

	api := rh.App.Group("/api/v1")
	authRoutes := api.Group("/auth")
	authRoutes.Post("/send-otp", h.SendOTP)
	authRoutes.Post("/verify-otp", h.VerifyOTP)
	authRoutes.Post("/refresh", h.Refresh)
	authRoutes.Post("/logout", h.Logout)
	authRoutes.Post("/logout-all", middleware.RequireAuth(authTokenService(h.auth), h.auth.ValidateAccessSession), h.LogoutAll)
	if cfg.DevOTPLog {
		authRoutes.Get("/dev/last-otp", h.LastDevOTP)
	}

	protected := api.Group("/users/me", middleware.RequireAuth(authTokenService(h.auth), h.auth.ValidateAccessSession))
	protected.Get("/", h.GetMe)
	protected.Patch("/", h.UpdateProfile)
	protected.Delete("/", h.DeleteAccount)
	protected.Post("/addresses", h.CreateAddress)
	protected.Get("/addresses", h.ListAddresses)
	protected.Post("/addresses/geocode", h.GeocodeAddress)
	protected.Get("/addresses/:addressID", h.GetAddress)
	protected.Patch("/addresses/:addressID", h.UpdateAddress)
	protected.Delete("/addresses/:addressID", h.DeleteAddress)
	protected.Put("/addresses/:addressID/default", h.SetDefaultAddress)

	SetupCatalogRoutes(rh, auth)
	SetupCommerceRoutes(rh, auth)
	SetupUploadsRoutes(rh, cfg, auth)
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

func (h *UserHandler) LastDevOTP(c *fiber.Ctx) error {
	phone := c.Query("phone")
	if phone == "" {
		return writeError(c, fiber.StatusBadRequest, "phone is required")
	}
	otp, ok := h.auth.LastDevOTP(phone)
	if !ok {
		return writeError(c, fiber.StatusNotFound, "no OTP generated yet for this phone")
	}
	return c.JSON(fiber.Map{"otp": otp})
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

func (h *UserHandler) GeocodeAddress(c *fiber.Ctx) error {
	var request dto.GeocodeRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	response, err := h.address.Geocode(request.Address)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(response)
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
	case errors.Is(err, repository.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return writeErrorCode(c, fiber.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, services.ErrNotServiceable):
		return writeErrorCode(c, fiber.StatusUnprocessableEntity, "not_serviceable", err.Error())
	case errors.Is(err, services.ErrOrderNotCancellable):
		return writeErrorCode(c, fiber.StatusConflict, "order_not_cancellable", err.Error())
	case errors.Is(err, services.ErrDeliveryCodeLocked):
		return writeErrorCode(c, fiber.StatusConflict, "delivery_code_locked", err.Error())
	case errors.Is(err, services.ErrActiveOrders):
		return writeErrorCode(c, fiber.StatusConflict, "active_orders", err.Error())
	case isInvalidDeliveryCode(err):
		return writeErrorCode(c, fiber.StatusUnprocessableEntity, "invalid_delivery_code", err.Error())
	case isCartUnavailable(err):
		var unavailable *services.CartUnavailableError
		errors.As(err, &unavailable)
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error(), "code": "cart_unavailable", "details": unavailable.Issues})
	case errors.Is(err, services.ErrCartStoreMismatch):
		return writeErrorCode(c, fiber.StatusConflict, "cart_store_mismatch", err.Error())
	case errors.Is(err, services.ErrInsufficientStock):
		return writeErrorCode(c, fiber.StatusConflict, "insufficient_stock", err.Error())
	case errors.Is(err, services.ErrEmptyCart):
		return writeErrorCode(c, fiber.StatusBadRequest, "empty_cart", err.Error())
	case errors.Is(err, services.ErrInvalidInput):
		// "invalid input: " is an internal prefix; clients get just the reason.
		return writeError(c, fiber.StatusBadRequest, strings.TrimPrefix(err.Error(), services.ErrInvalidInput.Error()+": "))
	case errors.Is(err, services.ErrInvalidPhone), errors.Is(err, services.ErrInvalidOTP), errors.Is(err, services.ErrOTPExpired), errors.Is(err, services.ErrOTPAttemptsExceeded), errors.Is(err, services.ErrInvalidRefreshToken), errors.Is(err, services.ErrSessionRevoked), errors.Is(err, services.ErrInvalidRole):
		return writeError(c, fiber.StatusBadRequest, err.Error())
	case errors.Is(err, repository.ErrOrderUnavailable), errors.Is(err, repository.ErrRiderBusy), errors.Is(err, gorm.ErrDuplicatedKey):
		return writeError(c, fiber.StatusConflict, err.Error())
	case errors.Is(err, services.ErrOTPTooSoon), errors.Is(err, services.ErrOTPRateLimited):
		return writeError(c, fiber.StatusTooManyRequests, err.Error())
	case errors.Is(err, services.ErrAddressNotFound):
		return writeError(c, fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, services.ErrGoogleMapsNotConfigured), errors.Is(err, services.ErrGoogleMaps):
		return writeError(c, fiber.StatusServiceUnavailable, "Google Maps geocoding is unavailable")
	case errors.Is(err, services.ErrOTPProvider):
		return writeError(c, fiber.StatusServiceUnavailable, "OTP provider is not configured")
	case errors.Is(err, services.ErrAccountDeleted):
		return writeError(c, fiber.StatusGone, err.Error())
	default:
		log.Printf("unhandled error on %s %s: %v", c.Method(), c.Path(), err)
		return writeError(c, fiber.StatusInternalServerError, "something went wrong, please try again")
	}
}

// errorCodes gives every status a stable machine-readable code so clients can
// branch on `code` instead of parsing messages.
var errorCodes = map[int]string{
	fiber.StatusBadRequest:            "invalid_input",
	fiber.StatusUnauthorized:          "unauthorized",
	fiber.StatusForbidden:             "forbidden",
	fiber.StatusNotFound:              "not_found",
	fiber.StatusMethodNotAllowed:      "method_not_allowed",
	fiber.StatusConflict:              "conflict",
	fiber.StatusGone:                  "gone",
	fiber.StatusRequestEntityTooLarge: "payload_too_large",
	fiber.StatusUnprocessableEntity:   "unprocessable",
	fiber.StatusTooManyRequests:       "rate_limited",
	fiber.StatusInternalServerError:   "internal",
	fiber.StatusServiceUnavailable:    "unavailable",
}

func writeError(c *fiber.Ctx, status int, message string) error {
	code := errorCodes[status]
	if code == "" {
		code = "error"
	}
	return writeErrorCode(c, status, code, message)
}

func writeErrorCode(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{"error": message, "code": code})
}

// JSONErrorHandler turns any error that escapes a handler (unknown route,
// oversized body, panic recovered upstream) into the standard JSON shape.
func JSONErrorHandler(c *fiber.Ctx, err error) error {
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		message := fiberErr.Message
		if fiberErr.Code == fiber.StatusRequestEntityTooLarge {
			message = "request is too large"
		}
		return writeError(c, fiberErr.Code, message)
	}
	return serviceError(c, err)
}

func isInvalidDeliveryCode(err error) bool {
	var target *services.InvalidDeliveryCodeError
	return errors.As(err, &target)
}

func isCartUnavailable(err error) bool {
	var target *services.CartUnavailableError
	return errors.As(err, &target)
}
