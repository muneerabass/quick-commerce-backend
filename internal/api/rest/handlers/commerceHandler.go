package handlers

import (
	"fmt"
	"strconv"
	"time"

	"quick-commerce/internal/api/middleware"
	"quick-commerce/internal/api/rest"
	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"
	"quick-commerce/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type CommerceHandler struct {
	cart   *services.CartService
	orders *services.OrderService
}

func SetupCommerceRoutes(rh *rest.RestHandler, auth *services.AuthService, deliveryCodeSecret string) {
	catalog := repository.NewCatalogRepository(rh.Db)
	commerce := repository.NewCommerceRepository(rh.Db)
	addresses := repository.NewAddressRepository(rh.Db)
	h := &CommerceHandler{
		cart:   services.NewCartService(rh.Db, catalog, commerce).WithAddresses(addresses),
		orders: services.NewOrderService(rh.Db, addresses, catalog, commerce).WithDeliveryCodeSecret(deliveryCodeSecret),
	}

	api := rh.App.Group("/api/v1")
	authenticated := middleware.RequireAuth(auth.TokenService(), auth.ValidateAccessSession)
	protected := api.Group("/", authenticated)
	protected.Get("cart", h.GetCart)
	cartWrites := userLimiter(60, time.Minute)
	protected.Post("cart/items", cartWrites, h.AddCartItem)
	protected.Patch("cart/items/:itemID", cartWrites, h.UpdateCartItem)
	protected.Delete("cart/items/:itemID", cartWrites, h.DeleteCartItem)
	protected.Delete("cart", cartWrites, h.ClearCart)
	protected.Post("orders", userLimiter(10, time.Minute), h.CreateOrder)
	protected.Get("orders", h.ListOrders)
	protected.Get("orders/:orderID", h.GetOrder)
	protected.Post("orders/:orderID/cancel", h.CancelOrder)

	delivery := api.Group("/delivery", authenticated, middleware.RequireRole(auth.UserRole, "delivery"))
	delivery.Get("/orders", h.PendingDeliveryOrders)
	// Registered before the :orderID param route so "mine" is not swallowed as an ID.
	delivery.Get("/orders/mine", h.MyDeliveryOrders)
	delivery.Get("/orders/:orderID", h.GetDeliveryOrder)
	delivery.Post("/orders/:orderID/accept", h.AcceptOrder)
	delivery.Post("/orders/:orderID/complete", h.CompleteOrder)
}

func (h *CommerceHandler) GetDeliveryOrder(c *fiber.Ctx) error {
	deliveryID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	orderID, err := uuid.Parse(c.Params("orderID"))
	if err != nil {
		return serviceError(c, services.ErrInvalidInput)
	}
	order, err := h.orders.GetForDelivery(deliveryID, orderID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(order)
}

func (h *CommerceHandler) GetCart(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	var addressID *uuid.UUID
	if value := c.Query("address_id"); value != "" {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return serviceError(c, services.ErrInvalidInput)
		}
		addressID = &parsed
	}
	response, err := h.cart.Get(userID, addressID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(response)
}

func (h *CommerceHandler) AddCartItem(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	var request dto.AddCartItemRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	response, err := h.cart.AddItem(userID, request)
	if err != nil {
		return serviceError(c, err)
	}
	return h.cartResponse(c, userID, response, fiber.StatusCreated)
}

// cartResponse returns a mutation's cart. When the client says which address it
// is checking out to (?address_id=), the cart is re-priced for it so serviceability
// issues stay current after every change.
func (h *CommerceHandler) cartResponse(c *fiber.Ctx, userID uuid.UUID, response dto.CartResponse, status int) error {
	if value := c.Query("address_id"); value != "" {
		if addressID, err := uuid.Parse(value); err == nil {
			if full, err := h.cart.Get(userID, &addressID); err == nil {
				response = full
			}
		}
	}
	return c.Status(status).JSON(response)
}

func (h *CommerceHandler) UpdateCartItem(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	itemID, err := uuid.Parse(c.Params("itemID"))
	if err != nil {
		return serviceError(c, services.ErrInvalidInput)
	}
	var request dto.UpdateCartItemRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	response, err := h.cart.UpdateItem(userID, itemID, request.Quantity)
	if err != nil {
		return serviceError(c, err)
	}
	return h.cartResponse(c, userID, response, fiber.StatusOK)
}

func (h *CommerceHandler) DeleteCartItem(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	itemID, err := uuid.Parse(c.Params("itemID"))
	if err != nil {
		return serviceError(c, services.ErrInvalidInput)
	}
	response, err := h.cart.DeleteItem(userID, itemID)
	if err != nil {
		return serviceError(c, err)
	}
	return h.cartResponse(c, userID, response, fiber.StatusOK)
}

func (h *CommerceHandler) ClearCart(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	if err := h.cart.Clear(userID); err != nil {
		return serviceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *CommerceHandler) CreateOrder(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	var request dto.CreateOrderRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	addressID, err := uuid.Parse(request.AddressID)
	if err != nil {
		return serviceError(c, services.ErrInvalidInput)
	}
	order, err := h.orders.Create(userID, addressID, c.Get("Idempotency-Key"))
	if err != nil {
		return serviceError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(order)
}

func (h *CommerceHandler) ListOrders(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	var before *time.Time
	if value := c.Query("before"); value != "" {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return serviceError(c, fmt.Errorf("%w: invalid before cursor", services.ErrInvalidInput))
		}
		before = &parsed
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	page, err := h.orders.List(userID, limit, before)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(page)
}

func (h *CommerceHandler) CancelOrder(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	orderID, err := uuid.Parse(c.Params("orderID"))
	if err != nil {
		return serviceError(c, services.ErrInvalidInput)
	}
	var request dto.CancelOrderRequest
	_ = c.BodyParser(&request) // the reason is optional
	order, err := h.orders.Cancel(userID, orderID, request.Reason)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(order)
}

func (h *CommerceHandler) GetOrder(c *fiber.Ctx) error {
	userID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	orderID, err := uuid.Parse(c.Params("orderID"))
	if err != nil {
		return serviceError(c, services.ErrInvalidInput)
	}
	order, err := h.orders.Get(userID, orderID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(order)
}

func (h *CommerceHandler) PendingDeliveryOrders(c *fiber.Ctx) error {
	deliveryID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	orders, err := h.orders.PendingForDelivery(deliveryID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(fiber.Map{"orders": orders})
}

// MyDeliveryOrders returns the rider's own orders by ?status=accepted|completed.
func (h *CommerceHandler) MyDeliveryOrders(c *fiber.Ctx) error {
	deliveryID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	status := c.Query("status", "accepted")
	orders, err := h.orders.MineForDelivery(deliveryID, status)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(fiber.Map{"orders": orders})
}

func (h *CommerceHandler) AcceptOrder(c *fiber.Ctx) error {
	deliveryID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	orderID, err := uuid.Parse(c.Params("orderID"))
	if err != nil {
		return serviceError(c, services.ErrInvalidInput)
	}
	order, err := h.orders.Accept(deliveryID, orderID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(order)
}

func (h *CommerceHandler) CompleteOrder(c *fiber.Ctx) error {
	deliveryID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	orderID, err := uuid.Parse(c.Params("orderID"))
	if err != nil {
		return serviceError(c, services.ErrInvalidInput)
	}
	var request dto.CompleteOrderRequest
	_ = c.BodyParser(&request)
	order, err := h.orders.Complete(deliveryID, orderID, request.DeliveryCode)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(order)
}
