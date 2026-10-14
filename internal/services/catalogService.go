package services

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"quick-commerce/internal/domain"
	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrInvalidRole       = errors.New("user does not have the required role")
	ErrCartStoreMismatch = errors.New("cart can contain products from only one store")
	ErrInsufficientStock = errors.New("insufficient stock")
	ErrEmptyCart         = errors.New("cart is empty")
	ErrInvalidOrderState = errors.New("order is not in the required state")
)

type CatalogService struct {
	catalog repository.CatalogRepository
}

func NewCatalogService(catalog repository.CatalogRepository) *CatalogService {
	return &CatalogService{catalog: catalog}
}

func (s *CatalogService) Nearby(latitude, longitude, radiusKM float64) ([]dto.NearbyStoreResponse, error) {
	if err := validateCoordinates(latitude, longitude); err != nil {
		return nil, err
	}
	if radiusKM <= 0 || radiusKM > 100 {
		return nil, fmt.Errorf("%w: radius must be between 0 and 100 km", ErrInvalidInput)
	}
	stores, err := s.catalog.ListActiveStores(nil)
	if err != nil {
		return nil, err
	}
	response := make([]dto.NearbyStoreResponse, 0)
	for _, store := range stores {
		if store.Latitude == nil || store.Longitude == nil {
			continue
		}
		distance := distanceKM(latitude, longitude, *store.Latitude, *store.Longitude)
		if distance > radiusKM {
			continue
		}
		products, err := s.catalog.ListProductsForStore(nil, store.ID)
		if err != nil {
			return nil, err
		}
		if len(products) == 0 {
			continue
		}
		mappedProducts := make([]dto.CatalogProductResponse, 0, len(products))
		for _, product := range products {
			var categoryID *string
			if product.CategoryID != nil {
				value := product.CategoryID.String()
				categoryID = &value
			}
			images := product.Images
			if images == nil {
				images = []string{}
			}
			mappedProducts = append(mappedProducts, dto.CatalogProductResponse{
				ID: product.ID.String(), StoreID: product.StoreID.String(), Name: product.Name, Description: product.Description,
				Price: product.Price, ImageURL: product.ImageURL, Images: images, CategoryID: categoryID, Quantity: product.Quantity,
			})
		}
		response = append(response, dto.NearbyStoreResponse{
			Store: dto.StoreResponse{ID: store.ID.String(), Name: store.Name, Address: store.Address,
				Latitude: store.Latitude, Longitude: store.Longitude, IsActive: store.IsActive, DistanceKM: math.Round(distance*100) / 100},
			Products: mappedProducts,
		})
	}
	return response, nil
}

type AdminService struct {
	db       *gorm.DB
	users    repository.UserRepository
	catalog  repository.CatalogRepository
	commerce repository.CommerceRepository
}

func NewAdminService(db *gorm.DB, users repository.UserRepository, catalog repository.CatalogRepository, commerce repository.CommerceRepository) *AdminService {
	return &AdminService{db: db, users: users, catalog: catalog, commerce: commerce}
}

func (s *AdminService) ListStores() ([]dto.StoreResponse, error) {
	stores, err := s.catalog.ListAllStores(nil)
	if err != nil {
		return nil, err
	}
	response := make([]dto.StoreResponse, 0, len(stores))
	for i := range stores {
		response = append(response, mapStore(&stores[i], 0))
	}
	return response, nil
}

// ListProducts returns products, optionally scoped to a single store. Each
// product now belongs to exactly one store, so admins browse items per store.
func (s *AdminService) ListProducts(storeID *uuid.UUID) ([]dto.CatalogProductResponse, error) {
	products, err := s.catalog.ListAllProducts(nil, storeID)
	if err != nil {
		return nil, err
	}
	// Attach per-store stock so the admin list shows live quantities.
	quantities := map[uuid.UUID]int{}
	if storeID != nil {
		if rows, err := s.catalog.ListInventoryForStore(nil, *storeID); err == nil {
			for _, row := range rows {
				quantities[row.ProductID] = row.Quantity
			}
		}
	}
	response := make([]dto.CatalogProductResponse, 0, len(products))
	for i := range products {
		response = append(response, mapProduct(&products[i], quantities[products[i].ID]))
	}
	return response, nil
}

func (s *AdminService) ListInventory(storeID uuid.UUID) ([]domain.Inventory, error) {
	if _, err := s.catalog.FindStore(nil, storeID); err != nil {
		return nil, err
	}
	return s.catalog.ListInventoryForStore(nil, storeID)
}

func (s *AdminService) ListCategories() ([]domain.Category, error) {
	return s.catalog.ListCategories(nil)
}

func (s *AdminService) CreateStore(request dto.StoreRequest) (dto.StoreResponse, error) {
	name, err := requiredText(request.Name, "name", 150)
	if err != nil {
		return dto.StoreResponse{}, err
	}
	address, err := requiredText(request.Address, "address", 500)
	if err != nil {
		return dto.StoreResponse{}, err
	}
	if request.Latitude == nil || request.Longitude == nil {
		return dto.StoreResponse{}, fmt.Errorf("%w: store latitude and longitude are required", ErrInvalidInput)
	}
	if err := validateCoordinates(*request.Latitude, *request.Longitude); err != nil {
		return dto.StoreResponse{}, err
	}
	isActive := true
	if request.IsActive != nil {
		isActive = *request.IsActive
	}
	store := &domain.Store{ID: uuid.New(), Name: name, Address: address, Latitude: request.Latitude, Longitude: request.Longitude, IsActive: isActive, CreatedAt: time.Now().UTC()}
	if err := s.catalog.CreateStore(nil, store); err != nil {
		return dto.StoreResponse{}, err
	}
	return mapStore(store, 0), nil
}

func (s *AdminService) UpdateStore(id uuid.UUID, request dto.StorePatchRequest) (dto.StoreResponse, error) {
	store, err := s.catalog.FindStore(nil, id)
	if err != nil {
		return dto.StoreResponse{}, err
	}
	if request.Name != nil {
		store.Name, err = requiredText(*request.Name, "name", 150)
		if err != nil {
			return dto.StoreResponse{}, err
		}
	}
	if request.Address != nil {
		store.Address, err = requiredText(*request.Address, "address", 500)
		if err != nil {
			return dto.StoreResponse{}, err
		}
	}
	if request.Latitude != nil {
		store.Latitude = request.Latitude
	}
	if request.Longitude != nil {
		store.Longitude = request.Longitude
	}
	if store.Latitude == nil || store.Longitude == nil {
		return dto.StoreResponse{}, fmt.Errorf("%w: store latitude and longitude are required", ErrInvalidInput)
	}
	if err := validateCoordinates(*store.Latitude, *store.Longitude); err != nil {
		return dto.StoreResponse{}, err
	}
	if request.IsActive != nil {
		store.IsActive = *request.IsActive
	}
	if err := s.catalog.UpdateStore(nil, store); err != nil {
		return dto.StoreResponse{}, err
	}
	return mapStore(store, 0), nil
}

func (s *AdminService) CreateProduct(request dto.ProductRequest) (dto.CatalogProductResponse, error) {
	name, err := requiredText(request.Name, "name", 200)
	if err != nil {
		return dto.CatalogProductResponse{}, err
	}
	if request.Price < 0 {
		return dto.CatalogProductResponse{}, fmt.Errorf("%w: price cannot be negative", ErrInvalidInput)
	}
	storeID, err := uuid.Parse(strings.TrimSpace(request.StoreID))
	if err != nil {
		return dto.CatalogProductResponse{}, fmt.Errorf("%w: store_id is required", ErrInvalidInput)
	}
	if _, err := s.catalog.FindStore(nil, storeID); err != nil {
		return dto.CatalogProductResponse{}, err
	}
	categoryID, err := optionalUUID(request.CategoryID)
	if err != nil {
		return dto.CatalogProductResponse{}, err
	}
	images := normalizeImages(request.Images)
	cover := coverImage(images, request.ImageURL)

	product := &domain.Product{ID: uuid.New(), StoreID: storeID, Name: name, Description: trimOptional(request.Description), Price: request.Price, ImageURL: cover, Images: images, CategoryID: categoryID, CreatedAt: time.Now().UTC()}
	if err := s.catalog.CreateProduct(nil, product); err != nil {
		return dto.CatalogProductResponse{}, err
	}

	// A product only shows at its store once it has stock, so seed inventory in
	// the same step. Defaults to 0 when no quantity was supplied.
	quantity := 0
	if request.Quantity != nil && *request.Quantity > 0 {
		quantity = *request.Quantity
	}
	inventory := &domain.Inventory{ID: uuid.New(), StoreID: storeID, ProductID: product.ID, Quantity: quantity, UpdatedAt: time.Now().UTC()}
	if err := s.catalog.UpsertInventory(nil, inventory); err != nil {
		return dto.CatalogProductResponse{}, err
	}
	return mapProduct(product, quantity), nil
}

func (s *AdminService) UpdateProduct(id uuid.UUID, request dto.ProductPatchRequest) (dto.CatalogProductResponse, error) {
	product, err := s.catalog.FindProduct(nil, id)
	if err != nil {
		return dto.CatalogProductResponse{}, err
	}
	if request.Name != nil {
		product.Name, err = requiredText(*request.Name, "name", 200)
		if err != nil {
			return dto.CatalogProductResponse{}, err
		}
	}
	if request.Description != nil {
		product.Description = trimOptional(request.Description)
	}
	if request.Price != nil {
		if *request.Price < 0 {
			return dto.CatalogProductResponse{}, fmt.Errorf("%w: price cannot be negative", ErrInvalidInput)
		}
		product.Price = *request.Price
	}
	if request.CategoryID != nil {
		product.CategoryID, err = optionalUUID(request.CategoryID)
		if err != nil {
			return dto.CatalogProductResponse{}, err
		}
	}
	if request.Images != nil {
		product.Images = normalizeImages(*request.Images)
		product.ImageURL = coverImage(product.Images, nil)
	}
	if request.ImageURL != nil {
		product.ImageURL = trimOptional(request.ImageURL)
	}
	if err := s.catalog.UpdateProduct(nil, product); err != nil {
		return dto.CatalogProductResponse{}, err
	}
	return mapProduct(product, 0), nil
}

// normalizeImages trims, drops blanks, and de-nils an image URL list.
func normalizeImages(in []string) []string {
	out := make([]string, 0, len(in))
	for _, url := range in {
		if trimmed := strings.TrimSpace(url); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// coverImage picks the primary image: the first in the list, else the legacy
// single image_url field when provided.
func coverImage(images []string, fallback *string) *string {
	if len(images) > 0 {
		value := images[0]
		return &value
	}
	return trimOptional(fallback)
}

func (s *AdminService) SetInventory(storeID, productID uuid.UUID, request dto.InventoryRequest) (domain.Inventory, error) {
	if request.Quantity < 0 {
		return domain.Inventory{}, fmt.Errorf("%w: quantity cannot be negative", ErrInvalidInput)
	}
	if _, err := s.catalog.FindStore(nil, storeID); err != nil {
		return domain.Inventory{}, err
	}
	if _, err := s.catalog.FindProduct(nil, productID); err != nil {
		return domain.Inventory{}, err
	}
	inventory := &domain.Inventory{ID: uuid.New(), StoreID: storeID, ProductID: productID, Quantity: request.Quantity, UpdatedAt: time.Now().UTC()}
	if err := s.catalog.UpsertInventory(nil, inventory); err != nil {
		return domain.Inventory{}, err
	}
	return *inventory, nil
}

func (s *AdminService) CreateDeliveryUser(request dto.DeliveryUserRequest) (dto.UserResponseDto, error) {
	phone, err := NormalizePhone(request.Phone)
	if err != nil {
		return dto.UserResponseDto{}, err
	}
	user, err := s.users.FindByPhone(nil, phone)
	if errors.Is(err, repository.ErrNotFound) {
		now := time.Now().UTC()
		user = &domain.User{ID: uuid.New(), Phone: phone, Role: "delivery", Status: "active", CreatedAt: now, UpdatedAt: now}
		if request.Name != nil {
			name := strings.TrimSpace(*request.Name)
			if len(name) > 120 {
				return dto.UserResponseDto{}, fmt.Errorf("%w: name must be at most 120 characters", ErrInvalidInput)
			}
			if name != "" {
				user.Name = &name
			}
		}
		if err := s.users.Create(nil, user); err != nil {
			return dto.UserResponseDto{}, err
		}
	} else if err != nil {
		return dto.UserResponseDto{}, err
	} else {
		if user.Role == "admin" {
			return dto.UserResponseDto{}, fmt.Errorf("%w: admin cannot be a delivery user", ErrInvalidRole)
		}
		user.Role = "delivery"
		if request.Name != nil {
			name := strings.TrimSpace(*request.Name)
			if len(name) > 120 {
				return dto.UserResponseDto{}, fmt.Errorf("%w: name must be at most 120 characters", ErrInvalidInput)
			}
			if name != "" {
				user.Name = &name
			}
		}
		user.UpdatedAt = time.Now().UTC()
		if err := s.users.Update(nil, user); err != nil {
			return dto.UserResponseDto{}, err
		}
	}
	return userResponse(user), nil
}

func (s *AdminService) AssignDeliveryUser(deliveryID, storeID uuid.UUID) error {
	user, err := s.users.FindByID(nil, deliveryID)
	if err != nil {
		return err
	}
	if user.Role != "delivery" {
		return fmt.Errorf("%w: user is not a delivery user", ErrInvalidRole)
	}
	if _, err := s.catalog.FindStore(nil, storeID); err != nil {
		return err
	}
	return s.commerce.CreateAssignment(nil, &domain.DeliveryStoreAssignment{ID: uuid.New(), DeliveryID: deliveryID, StoreID: storeID, CreatedAt: time.Now().UTC()})
}

func (s *AdminService) RemoveDeliveryAssignment(deliveryID, storeID uuid.UUID) error {
	return s.commerce.DeleteAssignment(nil, deliveryID, storeID)
}

func mapStore(store *domain.Store, distance float64) dto.StoreResponse {
	return dto.StoreResponse{ID: store.ID.String(), Name: store.Name, Address: store.Address, Latitude: store.Latitude, Longitude: store.Longitude, IsActive: store.IsActive, DistanceKM: distance}
}

func mapProduct(product *domain.Product, quantity int) dto.CatalogProductResponse {
	var categoryID *string
	if product.CategoryID != nil {
		value := product.CategoryID.String()
		categoryID = &value
	}
	images := product.Images
	if images == nil {
		images = []string{}
	}
	return dto.CatalogProductResponse{ID: product.ID.String(), StoreID: product.StoreID.String(), Name: product.Name, Description: product.Description, Price: product.Price, ImageURL: product.ImageURL, Images: images, CategoryID: categoryID, Quantity: quantity}
}

func validateCoordinates(latitude, longitude float64) error {
	if latitude < -90 || latitude > 90 {
		return fmt.Errorf("%w: latitude must be between -90 and 90", ErrInvalidInput)
	}
	if longitude < -180 || longitude > 180 {
		return fmt.Errorf("%w: longitude must be between -180 and 180", ErrInvalidInput)
	}
	return nil
}

func distanceKM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371.0
	lat1Rad, lat2Rad := lat1*math.Pi/180, lat2*math.Pi/180
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadius * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func requiredText(value, field string, max int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: %s is required", ErrInvalidInput, field)
	}
	if len(value) > max {
		return "", fmt.Errorf("%w: %s is too long", ErrInvalidInput, field)
	}
	return value, nil
}

func optionalUUID(value *string) (*uuid.UUID, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*value))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid UUID", ErrInvalidInput)
	}
	return &id, nil
}
