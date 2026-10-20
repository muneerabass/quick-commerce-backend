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

type CatalogHandler struct {
	catalog *services.CatalogService
	admin   *services.AdminService
	orders  *services.OrderService
}

func SetupCatalogRoutes(rh *rest.RestHandler, auth *services.AuthService, deliveryCodeSecret string) {
	catalogRepository := repository.NewCatalogRepository(rh.Db)
	commerceRepository := repository.NewCommerceRepository(rh.Db)
	admin := services.NewAdminService(rh.Db, repository.NewUserRepository(rh.Db), catalogRepository, commerceRepository)
	orders := services.NewOrderService(rh.Db, repository.NewAddressRepository(rh.Db), catalogRepository, commerceRepository).WithDeliveryCodeSecret(deliveryCodeSecret)
	h := &CatalogHandler{catalog: services.NewCatalogService(catalogRepository), admin: admin, orders: orders}

	api := rh.App.Group("/api/v1")
	api.Get("/catalog/products", h.Products)

	authenticated := middleware.RequireAuth(auth.TokenService(), auth.ValidateAccessSession)
	adminRoutes := api.Group("/admin", authenticated, middleware.RequireRole(auth.UserRole, "admin"))
	adminRoutes.Get("/stores", h.ListStores)
	adminRoutes.Post("/stores", h.CreateStore)
	adminRoutes.Patch("/stores/:storeID", h.UpdateStore)
	adminRoutes.Get("/products", h.ListProducts)
	adminRoutes.Post("/products", h.CreateProduct)
	adminRoutes.Patch("/products/:productID", h.UpdateProduct)
	adminRoutes.Get("/stores/:storeID/inventory", h.ListInventory)
	adminRoutes.Put("/stores/:storeID/inventory/:productID", h.SetInventory)
	adminRoutes.Get("/categories", h.ListCategories)
	adminRoutes.Get("/orders", h.ListOrders)
	adminRoutes.Post("/orders/:orderID/cancel", h.CancelOrder)
	adminRoutes.Post("/orders/:orderID/reassign", h.ReassignOrder)
	adminRoutes.Post("/delivery-users", h.CreateDeliveryUser)
	adminRoutes.Get("/stores/:storeID/delivery-users", h.ListDeliveryUsers)
	adminRoutes.Put("/delivery-users/:deliveryID/stores/:storeID", h.AssignDeliveryUser)
	adminRoutes.Delete("/delivery-users/:deliveryID/stores/:storeID", h.RemoveDeliveryAssignment)
}

func (h *CatalogHandler) ListStores(c *fiber.Ctx) error {
	stores, err := h.admin.ListStores()
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(fiber.Map{"stores": stores})
}

func (h *CatalogHandler) ListProducts(c *fiber.Ctx) error {
	var storeID *uuid.UUID
	if value := c.Query("store_id"); value != "" {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return writeError(c, fiber.StatusBadRequest, "invalid store_id")
		}
		storeID = &parsed
	}
	products, err := h.admin.ListProducts(storeID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(fiber.Map{"products": products})
}

func (h *CatalogHandler) ListInventory(c *fiber.Ctx) error {
	storeID, err := parseParamUUID(c, "storeID")
	if err != nil {
		return serviceError(c, err)
	}
	inventory, err := h.admin.ListInventory(storeID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(fiber.Map{"inventory": inventory})
}

func (h *CatalogHandler) ListCategories(c *fiber.Ctx) error {
	categories, err := h.admin.ListCategories()
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(fiber.Map{"categories": categories})
}

// Products returns the pincode catalog. An unserved pincode is a normal
// 200 with serviceable=false, not an error.
func (h *CatalogHandler) Products(c *fiber.Ctx) error {
	catalog, err := h.catalog.ProductsForPincode(c.Query("pincode"))
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(catalog)
}

func (h *CatalogHandler) CreateStore(c *fiber.Ctx) error {
	var request dto.StoreRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	store, err := h.admin.CreateStore(request)
	if err != nil {
		return serviceError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(store)
}

func (h *CatalogHandler) UpdateStore(c *fiber.Ctx) error {
	id, err := parseParamUUID(c, "storeID")
	if err != nil {
		return serviceError(c, err)
	}
	var request dto.StorePatchRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	store, err := h.admin.UpdateStore(id, request)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(store)
}

func (h *CatalogHandler) CreateProduct(c *fiber.Ctx) error {
	var request dto.ProductRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	product, err := h.admin.CreateProduct(request)
	if err != nil {
		return serviceError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(product)
}

func (h *CatalogHandler) UpdateProduct(c *fiber.Ctx) error {
	id, err := parseParamUUID(c, "productID")
	if err != nil {
		return serviceError(c, err)
	}
	var request dto.ProductPatchRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	product, err := h.admin.UpdateProduct(id, request)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(product)
}

func (h *CatalogHandler) SetInventory(c *fiber.Ctx) error {
	storeID, err := parseParamUUID(c, "storeID")
	if err != nil {
		return serviceError(c, err)
	}
	productID, err := parseParamUUID(c, "productID")
	if err != nil {
		return serviceError(c, err)
	}
	var request dto.InventoryRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	inventory, err := h.admin.SetInventory(storeID, productID, request)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(inventory)
}

func (h *CatalogHandler) CreateDeliveryUser(c *fiber.Ctx) error {
	var request dto.DeliveryUserRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	user, err := h.admin.CreateDeliveryUser(request)
	if err != nil {
		return serviceError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(user)
}

func (h *CatalogHandler) ListDeliveryUsers(c *fiber.Ctx) error {
	storeID, err := parseParamUUID(c, "storeID")
	if err != nil {
		return serviceError(c, err)
	}
	users, err := h.admin.ListDeliveryUsers(storeID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(fiber.Map{"delivery_users": users})
}

func (h *CatalogHandler) AssignDeliveryUser(c *fiber.Ctx) error {
	deliveryID, err := parseParamUUID(c, "deliveryID")
	if err != nil {
		return serviceError(c, err)
	}
	storeID, err := parseParamUUID(c, "storeID")
	if err != nil {
		return serviceError(c, err)
	}
	if err := h.admin.AssignDeliveryUser(deliveryID, storeID); err != nil {
		return serviceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *CatalogHandler) RemoveDeliveryAssignment(c *fiber.Ctx) error {
	deliveryID, err := parseParamUUID(c, "deliveryID")
	if err != nil {
		return serviceError(c, err)
	}
	storeID, err := parseParamUUID(c, "storeID")
	if err != nil {
		return serviceError(c, err)
	}
	if err := h.admin.RemoveDeliveryAssignment(deliveryID, storeID); err != nil {
		return serviceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func parseParamUUID(c *fiber.Ctx, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params(name))
	if err != nil {
		return uuid.Nil, services.ErrInvalidInput
	}
	return id, nil
}

// ListOrders is the admin order queue. ?stale=true shows accepted orders a rider
// has held longer than the stale threshold.
func (h *CatalogHandler) ListOrders(c *fiber.Ctx) error {
	filter := repository.OrderFilter{Status: c.Query("status")}
	switch filter.Status {
	case "", "pending", "accepted", "completed", "cancelled":
	default:
		return serviceError(c, fmt.Errorf("%w: unknown status", services.ErrInvalidInput))
	}
	if c.Query("stale") == "true" {
		cutoff := time.Now().UTC().Add(-services.StaleAcceptedAfter())
		filter.AcceptedBefore = &cutoff
	}
	if value := c.Query("store_id"); value != "" {
		id, err := uuid.Parse(value)
		if err != nil {
			return serviceError(c, services.ErrInvalidInput)
		}
		filter.StoreID = &id
	}
	if value := c.Query("before"); value != "" {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return serviceError(c, fmt.Errorf("%w: invalid before cursor", services.ErrInvalidInput))
		}
		filter.Before = &parsed
	}
	filter.Limit, _ = strconv.Atoi(c.Query("limit"))
	orders, err := h.orders.AdminList(filter)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(fiber.Map{"orders": orders})
}

func (h *CatalogHandler) CancelOrder(c *fiber.Ctx) error {
	adminID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	orderID, err := parseParamUUID(c, "orderID")
	if err != nil {
		return serviceError(c, err)
	}
	var request dto.CancelOrderRequest
	if err := c.BodyParser(&request); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}
	order, err := h.orders.AdminCancel(adminID, orderID, request.Reason)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(order)
}

func (h *CatalogHandler) ReassignOrder(c *fiber.Ctx) error {
	adminID, ok := middleware.UserID(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "authentication required")
	}
	orderID, err := parseParamUUID(c, "orderID")
	if err != nil {
		return serviceError(c, err)
	}
	order, err := h.orders.AdminReassign(adminID, orderID)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(order)
}
