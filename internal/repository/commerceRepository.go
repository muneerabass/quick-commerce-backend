package repository

import (
	"errors"
	"time"

	"quick-commerce/internal/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CatalogRepository interface {
	ListActiveStores(db *gorm.DB) ([]domain.Store, error)
	ListAllStores(db *gorm.DB) ([]domain.Store, error)
	ListAllProducts(db *gorm.DB, storeID *uuid.UUID) ([]domain.Product, error)
	ListInventoryForStore(db *gorm.DB, storeID uuid.UUID) ([]domain.Inventory, error)
	ListCategories(db *gorm.DB) ([]domain.Category, error)
	FindStore(db *gorm.DB, id uuid.UUID) (*domain.Store, error)
	CreateStore(db *gorm.DB, store *domain.Store) error
	UpdateStore(db *gorm.DB, store *domain.Store) error
	FindProduct(db *gorm.DB, id uuid.UUID) (*domain.Product, error)
	CreateProduct(db *gorm.DB, product *domain.Product) error
	UpdateProduct(db *gorm.DB, product *domain.Product) error
	ListProductsForStore(db *gorm.DB, storeID uuid.UUID) ([]domain.ProductInventory, error)
	FindInventoryForUpdate(db *gorm.DB, storeID, productID uuid.UUID) (*domain.Inventory, error)
	UpdateInventory(db *gorm.DB, inventory *domain.Inventory) error
	UpsertInventory(db *gorm.DB, inventory *domain.Inventory) error
}

type catalogRepository struct{ db *gorm.DB }

func NewCatalogRepository(db *gorm.DB) CatalogRepository { return &catalogRepository{db: db} }

func (r *catalogRepository) ListActiveStores(db *gorm.DB) ([]domain.Store, error) {
	var stores []domain.Store
	err := dbOrDefault(db, r.db).Where("is_active = ?", true).Order("created_at ASC").Find(&stores).Error
	return stores, err
}

func (r *catalogRepository) ListAllStores(db *gorm.DB) ([]domain.Store, error) {
	var stores []domain.Store
	err := dbOrDefault(db, r.db).Order("created_at ASC").Find(&stores).Error
	return stores, err
}

func (r *catalogRepository) ListAllProducts(db *gorm.DB, storeID *uuid.UUID) ([]domain.Product, error) {
	var products []domain.Product
	query := dbOrDefault(db, r.db).Order("name ASC")
	if storeID != nil {
		query = query.Where("store_id = ?", *storeID)
	}
	err := query.Find(&products).Error
	return products, err
}

func (r *catalogRepository) ListInventoryForStore(db *gorm.DB, storeID uuid.UUID) ([]domain.Inventory, error) {
	var rows []domain.Inventory
	err := dbOrDefault(db, r.db).Where("store_id = ?", storeID).Find(&rows).Error
	return rows, err
}

func (r *catalogRepository) ListCategories(db *gorm.DB) ([]domain.Category, error) {
	var rows []domain.Category
	err := dbOrDefault(db, r.db).Order("name ASC").Find(&rows).Error
	return rows, err
}

func (r *catalogRepository) FindStore(db *gorm.DB, id uuid.UUID) (*domain.Store, error) {
	var store domain.Store
	err := dbOrDefault(db, r.db).Where("id = ?", id).First(&store).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &store, err
}

func (r *catalogRepository) CreateStore(db *gorm.DB, store *domain.Store) error {
	if store.ID == uuid.Nil {
		store.ID = uuid.New()
	}
	return dbOrDefault(db, r.db).Create(store).Error
}

func (r *catalogRepository) UpdateStore(db *gorm.DB, store *domain.Store) error {
	return dbOrDefault(db, r.db).Model(&domain.Store{}).Where("id = ?", store.ID).Updates(map[string]any{
		"name": store.Name, "address": store.Address, "latitude": store.Latitude,
		"longitude": store.Longitude, "is_active": store.IsActive,
	}).Error
}

func (r *catalogRepository) FindProduct(db *gorm.DB, id uuid.UUID) (*domain.Product, error) {
	var product domain.Product
	err := dbOrDefault(db, r.db).Where("id = ?", id).First(&product).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &product, err
}

func (r *catalogRepository) CreateProduct(db *gorm.DB, product *domain.Product) error {
	if product.ID == uuid.Nil {
		product.ID = uuid.New()
	}
	return dbOrDefault(db, r.db).Create(product).Error
}

func (r *catalogRepository) UpdateProduct(db *gorm.DB, product *domain.Product) error {
	// GORM applies the `serializer:json` tag on Images when using the model API
	// with Select, so we update the full model rather than a raw column map.
	return dbOrDefault(db, r.db).Model(product).Where("id = ?", product.ID).
		Select("name", "description", "price", "image_url", "images", "category_id").
		Updates(product).Error
}

func (r *catalogRepository) ListProductsForStore(db *gorm.DB, storeID uuid.UUID) ([]domain.ProductInventory, error) {
	var rows []domain.ProductInventory
	err := dbOrDefault(db, r.db).Table("products").Select(
		"products.id, products.store_id, products.name, products.description, products.price, products.image_url, products.images, products.category_id, products.created_at, inventory.quantity",
	).Joins("JOIN inventory ON inventory.product_id = products.id AND inventory.store_id = ? AND inventory.quantity > 0", storeID).
		Order("products.name ASC").Scan(&rows).Error
	return rows, err
}

func (r *catalogRepository) FindInventoryForUpdate(db *gorm.DB, storeID, productID uuid.UUID) (*domain.Inventory, error) {
	var inventory domain.Inventory
	query := dbOrDefault(db, r.db).Where("store_id = ? AND product_id = ?", storeID, productID)
	if db != nil {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.First(&inventory).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &inventory, err
}

func (r *catalogRepository) UpdateInventory(db *gorm.DB, inventory *domain.Inventory) error {
	return dbOrDefault(db, r.db).Model(&domain.Inventory{}).Where("id = ?", inventory.ID).
		Updates(map[string]any{"quantity": inventory.Quantity, "updated_at": inventory.UpdatedAt}).Error
}

func (r *catalogRepository) UpsertInventory(db *gorm.DB, inventory *domain.Inventory) error {
	if inventory.ID == uuid.Nil {
		inventory.ID = uuid.New()
	}
	if inventory.UpdatedAt.IsZero() {
		inventory.UpdatedAt = time.Now().UTC()
	}
	return dbOrDefault(db, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "store_id"}, {Name: "product_id"}},
		DoUpdates: clause.Assignments(map[string]any{"quantity": inventory.Quantity, "updated_at": inventory.UpdatedAt}),
	}).Create(inventory).Error
}

type CommerceRepository interface {
	FindCart(db *gorm.DB, userID uuid.UUID, lock bool) (*domain.Cart, error)
	CreateCart(db *gorm.DB, cart *domain.Cart) error
	UpdateCart(db *gorm.DB, cart *domain.Cart) error
	ListCartItems(db *gorm.DB, cartID uuid.UUID) ([]domain.CartItem, error)
	FindCartItem(db *gorm.DB, cartID, itemID uuid.UUID) (*domain.CartItem, error)
	FindCartItemByProduct(db *gorm.DB, cartID, productID uuid.UUID, lock bool) (*domain.CartItem, error)
	CreateCartItem(db *gorm.DB, item *domain.CartItem) error
	UpdateCartItem(db *gorm.DB, item *domain.CartItem) error
	DeleteCartItem(db *gorm.DB, cartID, itemID uuid.UUID) error
	ClearCart(db *gorm.DB, cartID uuid.UUID) error
	CreateOrder(db *gorm.DB, order *domain.Order, items []domain.OrderItem) error
	ListOrdersForUser(db *gorm.DB, userID uuid.UUID) ([]domain.Order, error)
	FindOrderForUser(db *gorm.DB, userID, orderID uuid.UUID) (*domain.Order, error)
	ListOrderItems(db *gorm.DB, orderID uuid.UUID) ([]domain.OrderItem, error)
	ListPendingOrdersForDelivery(db *gorm.DB, deliveryID uuid.UUID) ([]domain.Order, error)
	ListOrdersForDeliveryBy(db *gorm.DB, deliveryID uuid.UUID, status string) ([]domain.Order, error)
	AcceptOrder(db *gorm.DB, orderID, deliveryID uuid.UUID, now time.Time) error
	CompleteOrder(db *gorm.DB, orderID, deliveryID uuid.UUID, now time.Time) error
	CreateAssignment(db *gorm.DB, assignment *domain.DeliveryStoreAssignment) error
	DeleteAssignment(db *gorm.DB, deliveryID, storeID uuid.UUID) error
	IsAssigned(db *gorm.DB, deliveryID, storeID uuid.UUID) (bool, error)
}

type commerceRepository struct{ db *gorm.DB }

func NewCommerceRepository(db *gorm.DB) CommerceRepository { return &commerceRepository{db: db} }

func (r *commerceRepository) FindCart(db *gorm.DB, userID uuid.UUID, lock bool) (*domain.Cart, error) {
	var cart domain.Cart
	query := dbOrDefault(db, r.db).Where("user_id = ?", userID)
	if lock && db != nil {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.First(&cart).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &cart, err
}

func (r *commerceRepository) CreateCart(db *gorm.DB, cart *domain.Cart) error {
	if cart.ID == uuid.Nil {
		cart.ID = uuid.New()
	}
	return dbOrDefault(db, r.db).Create(cart).Error
}

func (r *commerceRepository) UpdateCart(db *gorm.DB, cart *domain.Cart) error {
	return dbOrDefault(db, r.db).Model(&domain.Cart{}).Where("id = ?", cart.ID).
		Updates(map[string]any{"store_id": cart.StoreID, "updated_at": cart.UpdatedAt}).Error
}

func (r *commerceRepository) ListCartItems(db *gorm.DB, cartID uuid.UUID) ([]domain.CartItem, error) {
	var items []domain.CartItem
	err := dbOrDefault(db, r.db).Where("cart_id = ?", cartID).Order("created_at ASC").Find(&items).Error
	return items, err
}

func (r *commerceRepository) FindCartItem(db *gorm.DB, cartID, itemID uuid.UUID) (*domain.CartItem, error) {
	var item domain.CartItem
	err := dbOrDefault(db, r.db).Where("cart_id = ? AND id = ?", cartID, itemID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &item, err
}

func (r *commerceRepository) FindCartItemByProduct(db *gorm.DB, cartID, productID uuid.UUID, lock bool) (*domain.CartItem, error) {
	var item domain.CartItem
	query := dbOrDefault(db, r.db).Where("cart_id = ? AND product_id = ?", cartID, productID)
	if lock && db != nil {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &item, err
}

func (r *commerceRepository) CreateCartItem(db *gorm.DB, item *domain.CartItem) error {
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	return dbOrDefault(db, r.db).Create(item).Error
}

func (r *commerceRepository) UpdateCartItem(db *gorm.DB, item *domain.CartItem) error {
	return dbOrDefault(db, r.db).Model(&domain.CartItem{}).Where("id = ?", item.ID).
		Updates(map[string]any{"quantity": item.Quantity, "updated_at": item.UpdatedAt}).Error
}

func (r *commerceRepository) DeleteCartItem(db *gorm.DB, cartID, itemID uuid.UUID) error {
	query := dbOrDefault(db, r.db).Where("cart_id = ? AND id = ?", cartID, itemID).Delete(&domain.CartItem{})
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *commerceRepository) ClearCart(db *gorm.DB, cartID uuid.UUID) error {
	return dbOrDefault(db, r.db).Where("cart_id = ?", cartID).Delete(&domain.CartItem{}).Error
}

func (r *commerceRepository) CreateOrder(db *gorm.DB, order *domain.Order, items []domain.OrderItem) error {
	if order.ID == uuid.Nil {
		order.ID = uuid.New()
	}
	if err := dbOrDefault(db, r.db).Create(order).Error; err != nil {
		return err
	}
	for i := range items {
		if items[i].ID == uuid.Nil {
			items[i].ID = uuid.New()
		}
		items[i].OrderID = order.ID
	}
	return dbOrDefault(db, r.db).Create(&items).Error
}

func (r *commerceRepository) ListOrdersForUser(db *gorm.DB, userID uuid.UUID) ([]domain.Order, error) {
	var orders []domain.Order
	err := dbOrDefault(db, r.db).Where("user_id = ?", userID).Order("created_at DESC").Find(&orders).Error
	return orders, err
}

func (r *commerceRepository) FindOrderForUser(db *gorm.DB, userID, orderID uuid.UUID) (*domain.Order, error) {
	var order domain.Order
	err := dbOrDefault(db, r.db).Where("user_id = ? AND id = ?", userID, orderID).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &order, err
}

func (r *commerceRepository) ListOrderItems(db *gorm.DB, orderID uuid.UUID) ([]domain.OrderItem, error) {
	var items []domain.OrderItem
	err := dbOrDefault(db, r.db).Where("order_id = ?", orderID).Order("id ASC").Find(&items).Error
	return items, err
}

func (r *commerceRepository) ListPendingOrdersForDelivery(db *gorm.DB, deliveryID uuid.UUID) ([]domain.Order, error) {
	var orders []domain.Order
	err := dbOrDefault(db, r.db).Table("orders").Joins(
		"JOIN delivery_store_assignments dsa ON dsa.store_id = orders.store_id AND dsa.delivery_id = ?", deliveryID,
	).Where("orders.status = ?", "pending").Order("orders.created_at ASC").Find(&orders).Error
	return orders, err
}

func (r *commerceRepository) AcceptOrder(db *gorm.DB, orderID, deliveryID uuid.UUID, now time.Time) error {
	base := dbOrDefault(db, r.db)

	// A rider may hold only one active delivery at a time. Check first so we can
	// return a clear "finish your current delivery" error; the NOT EXISTS clause
	// in the UPDATE below keeps it race-safe if two accepts land concurrently.
	var activeCount int64
	if err := base.Model(&domain.Order{}).Where("accepted_by = ? AND status = ?", deliveryID, "accepted").Count(&activeCount).Error; err != nil {
		return err
	}
	if activeCount > 0 {
		return ErrRiderBusy
	}

	query := base.Model(&domain.Order{}).Where(
		"id = ? AND status = 'pending'"+
			" AND NOT EXISTS (SELECT 1 FROM orders o2 WHERE o2.accepted_by = ? AND o2.status = 'accepted')"+
			" AND EXISTS (SELECT 1 FROM delivery_store_assignments dsa WHERE dsa.store_id = orders.store_id AND dsa.delivery_id = ?)",
		orderID, deliveryID, deliveryID,
	).Updates(map[string]any{"status": "accepted", "accepted_by": deliveryID, "accepted_at": now, "updated_at": now})
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected == 0 {
		return ErrOrderUnavailable
	}
	return nil
}

// ListOrdersForDeliveryBy returns a rider's own orders filtered by status
// (e.g. "accepted" active deliveries, or "completed" history).
func (r *commerceRepository) ListOrdersForDeliveryBy(db *gorm.DB, deliveryID uuid.UUID, status string) ([]domain.Order, error) {
	var orders []domain.Order
	err := dbOrDefault(db, r.db).Where("accepted_by = ? AND status = ?", deliveryID, status).
		Order("updated_at DESC").Find(&orders).Error
	return orders, err
}

func (r *commerceRepository) CompleteOrder(db *gorm.DB, orderID, deliveryID uuid.UUID, now time.Time) error {
	query := dbOrDefault(db, r.db).Model(&domain.Order{}).Where(
		"id = ? AND status = 'accepted' AND accepted_by = ?", orderID, deliveryID,
	).Updates(map[string]any{"status": "completed", "payment_status": "paid", "completed_at": now, "updated_at": now})
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected == 0 {
		return ErrOrderUnavailable
	}
	return nil
}

func (r *commerceRepository) CreateAssignment(db *gorm.DB, assignment *domain.DeliveryStoreAssignment) error {
	if assignment.ID == uuid.Nil {
		assignment.ID = uuid.New()
	}
	return dbOrDefault(db, r.db).Create(assignment).Error
}

func (r *commerceRepository) DeleteAssignment(db *gorm.DB, deliveryID, storeID uuid.UUID) error {
	query := dbOrDefault(db, r.db).Where("delivery_id = ? AND store_id = ?", deliveryID, storeID).Delete(&domain.DeliveryStoreAssignment{})
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *commerceRepository) IsAssigned(db *gorm.DB, deliveryID, storeID uuid.UUID) (bool, error) {
	var count int64
	err := dbOrDefault(db, r.db).Model(&domain.DeliveryStoreAssignment{}).
		Where("delivery_id = ? AND store_id = ?", deliveryID, storeID).Count(&count).Error
	return count > 0, err
}

var ErrOrderUnavailable = errors.New("order is no longer available")

// ErrRiderBusy is returned when a delivery rider tries to accept a new order
// while they already have an active (accepted, not yet completed) delivery.
var ErrRiderBusy = errors.New("finish your current delivery before accepting another order")
