package handlers

import (
	"strconv"

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
}

func SetupCatalogRoutes(rh *rest.RestHandler, auth *services.AuthService) {
	catalogRepository := repository.NewCatalogRepository(rh.Db)
	commerceRepository := repository.NewCommerceRepository(rh.Db)
	admin := services.NewAdminService(rh.Db, repository.NewUserRepository(rh.Db), catalogRepository, commerceRepository)
	h := &CatalogHandler{catalog: services.NewCatalogService(catalogRepository), admin: admin}

	api := rh.App.Group("/api/v1")
	api.Get("/catalog/nearby", h.Nearby)

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
	adminRoutes.Post("/delivery-users", h.CreateDeliveryUser)
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
	products, err := h.admin.ListProducts()
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

func (h *CatalogHandler) Nearby(c *fiber.Ctx) error {
	latitude, err := strconv.ParseFloat(c.Query("latitude"), 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "latitude is required")
	}
	longitude, err := strconv.ParseFloat(c.Query("longitude"), 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "longitude is required")
	}
	radius := 10.0
	if value := c.Query("radius_km"); value != "" {
		radius, err = strconv.ParseFloat(value, 64)
		if err != nil {
			return writeError(c, fiber.StatusBadRequest, "invalid radius_km")
		}
	}
	stores, err := h.catalog.Nearby(latitude, longitude, radius)
	if err != nil {
		return serviceError(c, err)
	}
	return c.JSON(fiber.Map{"stores": stores})
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
