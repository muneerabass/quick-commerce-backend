package handlers

import (
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

func SetupCommerceRoutes(rh *rest.RestHandler, auth *services.AuthService) {
	catalog := repository.NewCatalogRepository(rh.Db)
	commerce := repository.NewCommerceRepository(rh.Db)
	addresses := repository.NewAddressRepository(rh.Db)
	h := &CommerceHandler{cart: services.NewCartService(rh.Db, catalog, commerce), orders: services.NewOrderService(rh.Db, addresses, catalog, commerce)}

	api := rh.App.Group("/api/v1")
	authenticated := middleware.RequireAuth(auth.TokenService(), auth.ValidateAccessSession)
	protected := api.Group("/", authenticated)
	protected.Get("cart", h.GetCart)
	protected.Post("cart/items", h.AddCartItem)
	protected.Patch("cart/items/:itemID", h.UpdateCartItem)
	protected.Delete("cart/items/:itemID", h.DeleteCartItem)
	protected.Delete("cart", h.ClearCart)
	protected.Post("orders", h.CreateOrder)
	protected.Get("orders", h.ListOrders)
	protected.Get("orders/:orderID", h.GetOrder)

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
	response, err := h.cart.Get(userID)
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
	return c.Status(fiber.StatusCreated).JSON(response)
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
	return c.JSON(response)
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
	return c.JSON(response)
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
	order, err := h.orders.Create(userID, addressID)
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
	orders, err := h.orders.List(userID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(fiber.Map{"orders": orders})
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
	order, err := h.orders.Complete(deliveryID, orderID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(order)
}
