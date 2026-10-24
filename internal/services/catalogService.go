package services

import (
	"errors"
	"fmt"
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
	ErrNotServiceable    = errors.New("this store does not deliver to the selected pincode")
)

// NotServiceableError carries which store and pincode failed so the message can
// name them. It matches ErrNotServiceable under errors.Is.
type NotServiceableError struct{ Store, Pincode string }

func (e *NotServiceableError) Error() string {
	return fmt.Sprintf("%s doesn't deliver to %s. Choose a different address or pincode.", e.Store, e.Pincode)
}

func (e *NotServiceableError) Is(target error) bool { return target == ErrNotServiceable }

type CatalogService struct {
	catalog repository.CatalogRepository
}

func NewCatalogService(catalog repository.CatalogRepository) *CatalogService {
	return &CatalogService{catalog: catalog}
}

// ProductsForPincode builds the home catalog for a pincode: the union of
// in-stock products across every active store that serves it. A pincode no
// store serves is not an error; it returns serviceable=false so clients can show
// a dedicated "we're not here yet" state.
func (s *CatalogService) ProductsForPincode(pincode string) (dto.PincodeCatalogResponse, error) {
	pincode, err := normalizePincode(pincode)
	if err != nil {
		return dto.PincodeCatalogResponse{}, err
	}
	response := dto.PincodeCatalogResponse{Pincode: pincode, Stores: []dto.StoreSummary{}, Products: []dto.CatalogProductResponse{}}
	stores, err := s.catalog.ListStoresForPincode(nil, pincode)
	if err != nil {
		return dto.PincodeCatalogResponse{}, err
	}
	if len(stores) == 0 {
		return response, nil
	}
	response.Serviceable = true
	for _, store := range stores {
		response.Stores = append(response.Stores, dto.StoreSummary{ID: store.ID.String(), Name: store.Name})
	}
	products, err := s.catalog.ListProductsForPincode(nil, pincode)
	if err != nil {
		return dto.PincodeCatalogResponse{}, err
	}
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
		response.Products = append(response.Products, dto.CatalogProductResponse{
			ID: product.ID.String(), StoreID: product.StoreID.String(), StoreName: product.StoreName, Name: product.Name, Description: product.Description,
			Price: product.Price, ImageURL: product.ImageURL, Images: images, CategoryID: categoryID, CategoryName: product.CategoryName, Quantity: product.Quantity,
		})
	}
	return response, nil
}

// GetProduct returns the full catalog detail for a single product, including
// its store name, category name, and live stock quantity. It is used by the
// product-detail screen when the product is not yet in the client catalog cache.
func (s *CatalogService) GetProduct(id uuid.UUID) (dto.CatalogProductResponse, error) {
	product, err := s.catalog.FindProductInventory(nil, id)
	if err != nil {
		return dto.CatalogProductResponse{}, err
	}
	var categoryID *string
	if product.CategoryID != nil {
		value := product.CategoryID.String()
		categoryID = &value
	}
	images := product.Images
	if images == nil {
		images = []string{}
	}
	return dto.CatalogProductResponse{
		ID:           product.ID.String(),
		StoreID:      product.StoreID.String(),
		StoreName:    product.StoreName,
		Name:         product.Name,
		Description:  product.Description,
		Price:        product.Price,
		ImageURL:     product.ImageURL,
		Images:       images,
		CategoryID:   categoryID,
		CategoryName: product.CategoryName,
		Quantity:     product.Quantity,
	}, nil
}

type AdminService struct {
	db       *gorm.DB
	users    repository.UserRepository
	catalog  repository.CatalogRepository
	commerce repository.CommerceRepository
	sessions repository.SessionRepository
}

func NewAdminService(db *gorm.DB, users repository.UserRepository, catalog repository.CatalogRepository, commerce repository.CommerceRepository, sessions repository.SessionRepository) *AdminService {
	return &AdminService{db: db, users: users, catalog: catalog, commerce: commerce, sessions: sessions}
}

func (s *AdminService) ListStores() ([]dto.StoreResponse, error) {
	stores, err := s.catalog.ListAllStores(nil)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(stores))
	for i := range stores {
		ids = append(ids, stores[i].ID)
	}
	pincodes, err := s.catalog.ListPincodes(nil, ids)
	if err != nil {
		return nil, err
	}
	response := make([]dto.StoreResponse, 0, len(stores))
	for i := range stores {
		stores[i].Pincodes = pincodes[stores[i].ID]
		response = append(response, mapStore(&stores[i]))
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
	storeNames := map[uuid.UUID]string{}
	if stores, err := s.catalog.ListAllStores(nil); err == nil {
		for _, store := range stores {
			storeNames[store.ID] = store.Name
		}
	}
	response := make([]dto.CatalogProductResponse, 0, len(products))
	for i := range products {
		mapped := mapProduct(&products[i], quantities[products[i].ID])
		mapped.StoreName = storeNames[products[i].StoreID]
		response = append(response, mapped)
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
	pincodes, err := normalizePincodes(request.Pincodes)
	if err != nil {
		return dto.StoreResponse{}, err
	}
	if len(pincodes) == 0 {
		return dto.StoreResponse{}, fmt.Errorf("%w: add at least one serviceable pincode", ErrInvalidInput)
	}
	isActive := true
	if request.IsActive != nil {
		isActive = *request.IsActive
	}
	if exists, err := s.catalog.StoreNameExists(nil, name, nil); err != nil {
		return dto.StoreResponse{}, err
	} else if exists {
		return dto.StoreResponse{}, fmt.Errorf("%w: a store named %q already exists", ErrDuplicateName, name)
	}
	store := &domain.Store{ID: uuid.New(), Name: name, Address: address, IsActive: isActive, CreatedAt: time.Now().UTC(), Pincodes: pincodes}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.catalog.CreateStore(tx, store); err != nil {
			return err
		}
		return s.catalog.ReplacePincodes(tx, store.ID, pincodes)
	})
	if err != nil {
		return dto.StoreResponse{}, err
	}
	return mapStore(store), nil
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
		if exists, err := s.catalog.StoreNameExists(nil, store.Name, &id); err != nil {
			return dto.StoreResponse{}, err
		} else if exists {
			return dto.StoreResponse{}, fmt.Errorf("%w: a store named %q already exists", ErrDuplicateName, store.Name)
		}
	}
	if request.Address != nil {
		store.Address, err = requiredText(*request.Address, "address", 500)
		if err != nil {
			return dto.StoreResponse{}, err
		}
	}
	if request.IsActive != nil {
		store.IsActive = *request.IsActive
	}
	var newPincodes []string
	if request.Pincodes != nil {
		newPincodes, err = normalizePincodes(*request.Pincodes)
		if err != nil {
			return dto.StoreResponse{}, err
		}
		if len(newPincodes) == 0 {
			return dto.StoreResponse{}, fmt.Errorf("%w: a store needs at least one serviceable pincode", ErrInvalidInput)
		}
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.catalog.UpdateStore(tx, store); err != nil {
			return err
		}
		if request.Pincodes != nil {
			return s.catalog.ReplacePincodes(tx, store.ID, newPincodes)
		}
		return nil
	})
	if err != nil {
		return dto.StoreResponse{}, err
	}
	if request.Pincodes != nil {
		store.Pincodes = newPincodes
	} else {
		existing, err := s.catalog.ListPincodes(nil, []uuid.UUID{store.ID})
		if err != nil {
			return dto.StoreResponse{}, err
		}
		store.Pincodes = existing[store.ID]
	}
	response := mapStore(store)
	if !store.IsActive {
		// Open orders keep being served; tell the admin how many are in flight.
		if open, err := s.commerce.CountOpenOrdersForStore(nil, store.ID); err == nil {
			response.OpenOrders = int(open)
		}
	}
	return response, nil
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
	store, err := s.catalog.FindStore(nil, storeID)
	if err != nil {
		return dto.CatalogProductResponse{}, err
	}
	categoryID, err := optionalUUID(request.CategoryID)
	if err != nil {
		return dto.CatalogProductResponse{}, err
	}
	if exists, err := s.catalog.ProductNameExistsInStore(nil, storeID, name, nil); err != nil {
		return dto.CatalogProductResponse{}, err
	} else if exists {
		return dto.CatalogProductResponse{}, fmt.Errorf("%w: a product named %q already exists in this store", ErrDuplicateName, name)
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
	created := mapProduct(product, quantity)
	created.StoreName = store.Name
	return created, nil
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
		if exists, err := s.catalog.ProductNameExistsInStore(nil, product.StoreID, product.Name, &id); err != nil {
			return dto.CatalogProductResponse{}, err
		} else if exists {
			return dto.CatalogProductResponse{}, fmt.Errorf("%w: a product named %q already exists in this store", ErrDuplicateName, product.Name)
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
	updated := mapProduct(product, 0)
	if store, err := s.catalog.FindStore(nil, product.StoreID); err == nil {
		updated.StoreName = store.Name
	}
	return updated, nil
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

	// Validate optional store_id up-front, before opening the transaction.
	var storeID *uuid.UUID
	if request.StoreID != nil {
		sid, err := uuid.Parse(strings.TrimSpace(*request.StoreID))
		if err != nil {
			return dto.UserResponseDto{}, fmt.Errorf("%w: store_id is not a valid UUID", ErrInvalidInput)
		}
		if _, err := s.catalog.FindStore(nil, sid); err != nil {
			return dto.UserResponseDto{}, err
		}
		storeID = &sid
	}

	var user *domain.User
	// H-004: wrap user upsert and the optional store assignment in a single
	// transaction so a failed assignment can never leave an orphaned rider.
	err = s.db.Transaction(func(tx *gorm.DB) error {
		existing, findErr := s.users.FindByPhone(tx, phone)
		if errors.Is(findErr, repository.ErrNotFound) {
			now := time.Now().UTC()
			existing = &domain.User{ID: uuid.New(), Phone: phone, Role: "delivery", Status: "active", CreatedAt: now, UpdatedAt: now}
			if request.Name != nil {
				name := strings.TrimSpace(*request.Name)
				if len(name) > 120 {
					return fmt.Errorf("%w: name must be at most 120 characters", ErrInvalidInput)
				}
				if name != "" {
					existing.Name = &name
				}
			}
			if err := s.users.Create(tx, existing); err != nil {
				return err
			}
		} else if findErr != nil {
			return findErr
		} else {
			if existing.Role == "admin" {
				return fmt.Errorf("%w: admin cannot be a delivery user", ErrInvalidRole)
			}
			existing.Role = "delivery"
			if request.Name != nil {
				name := strings.TrimSpace(*request.Name)
				if len(name) > 120 {
					return fmt.Errorf("%w: name must be at most 120 characters", ErrInvalidInput)
				}
				if name != "" {
					existing.Name = &name
				}
			}
			existing.UpdatedAt = time.Now().UTC()
			if err := s.users.Update(tx, existing); err != nil {
				return err
			}
		}
		user = existing
		if storeID != nil {
			already, err := s.commerce.IsAssigned(tx, user.ID, *storeID)
			if err != nil {
				return err
			}
			if !already {
				if err := s.commerce.CreateAssignment(tx, &domain.DeliveryStoreAssignment{
					ID: uuid.New(), DeliveryID: user.ID, StoreID: *storeID, CreatedAt: time.Now().UTC(),
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return dto.UserResponseDto{}, err
	}
	return userResponse(user), nil
}

func (s *AdminService) ListDeliveryUsers(storeID uuid.UUID) ([]dto.UserResponseDto, error) {
	users, err := s.users.ListDeliveryForStore(nil, storeID)
	if err != nil {
		return nil, err
	}
	result := make([]dto.UserResponseDto, 0, len(users))
	for _, u := range users {
		result = append(result, userResponse(u))
	}
	return result, nil
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
	// Taking a rider off a store while they hold one of its orders would strand it.
	active, err := s.commerce.CountActiveOrdersForRider(nil, deliveryID, &storeID)
	if err != nil {
		return err
	}
	if active > 0 {
		return ErrActiveOrders
	}
	return s.commerce.DeleteAssignment(nil, deliveryID, storeID)
}

// DeleteDeliveryUser demotes a delivery user back to a customer: unassigns them
// from every store, flips users.role to "customer", and revokes every session
// so the rider app on the next request gets a 401 → auto-signout.
// Refuses when the rider still holds an active order anywhere.
func (s *AdminService) DeleteDeliveryUser(deliveryID uuid.UUID) error {
	user, err := s.users.FindByID(nil, deliveryID)
	if err != nil {
		return err
	}
	if user.Role != "delivery" {
		return fmt.Errorf("%w: user is not a delivery user", ErrInvalidRole)
	}
	active, err := s.commerce.CountActiveOrdersForRider(nil, deliveryID, nil)
	if err != nil {
		return err
	}
	if active > 0 {
		return ErrActiveOrders
	}
	now := time.Now().UTC()
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.commerce.DeleteAllAssignmentsForDelivery(tx, deliveryID); err != nil {
			return err
		}
		user.Role = "customer"
		user.UpdatedAt = now
		if err := s.users.Update(tx, user); err != nil {
			return err
		}
		return s.sessions.RevokeAll(tx, deliveryID, now)
	})
}

func mapStore(store *domain.Store) dto.StoreResponse {
	pincodes := store.Pincodes
	if pincodes == nil {
		pincodes = []string{}
	}
	return dto.StoreResponse{ID: store.ID.String(), Name: store.Name, Address: store.Address, Pincodes: pincodes, IsActive: store.IsActive}
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

// normalizePincode trims a pincode and checks it is 4-10 digits.
func normalizePincode(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: pincode is required", ErrInvalidInput)
	}
	if !pincodePattern.MatchString(value) {
		return "", fmt.Errorf("%w: pincode must contain 4 to 10 digits", ErrInvalidInput)
	}
	return value, nil
}

const maxPincodesPerStore = 200

// normalizePincodes validates, trims and de-duplicates an admin-supplied list,
// keeping the original order.
func normalizePincodes(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		pincode, err := normalizePincode(raw)
		if err != nil {
			return nil, fmt.Errorf("%w (got %q)", err, strings.TrimSpace(raw))
		}
		if seen[pincode] {
			continue
		}
		seen[pincode] = true
		out = append(out, pincode)
	}
	if len(out) > maxPincodesPerStore {
		return nil, fmt.Errorf("%w: a store can serve at most %d pincodes", ErrInvalidInput, maxPincodesPerStore)
	}
	return out, nil
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
