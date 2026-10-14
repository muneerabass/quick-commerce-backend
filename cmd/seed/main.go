package main

// Idempotent seed for local development:
//   - 1 admin user (phone from ADMIN_PHONE)
//   - 1 delivery rider (+919000000001)
//   - 2 categories, 1 store, 6 products with image URLs, inventory, delivery assignment.
//
// Safe to re-run: existing rows (matched by natural key) are preserved and
// inventory is upserted on (store_id, product_id).
//
// Run: go run ./cmd/seed

import (
	"fmt"
	"log"
	"time"

	"quick-commerce/configs"
	"quick-commerce/internal/domain"
	"quick-commerce/internal/migrations"
	"quick-commerce/internal/services"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type productSeed struct {
	Name        string
	Description string
	PricePaise  int
	Category    string
	ImageURL    string
	StockQty    int
}

func main() {
	cfg, err := configs.LoadSettings()
	if err != nil {
		log.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{})
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	if err := migrations.Run(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	if cfg.AdminPhone == "" {
		log.Fatal("ADMIN_PHONE is required to seed an admin user")
	}
	adminPhone, err := services.NormalizePhone(cfg.AdminPhone)
	if err != nil {
		log.Fatalf("invalid ADMIN_PHONE: %v", err)
	}

	now := time.Now().UTC()

	// Admin
	admin, err := upsertUserByPhone(db, adminPhone, "admin", "Dev Admin", now)
	if err != nil {
		log.Fatalf("admin: %v", err)
	}
	// Delivery rider
	rider, err := upsertUserByPhone(db, "+919000000001", "delivery", "Dev Rider", now)
	if err != nil {
		log.Fatalf("rider: %v", err)
	}

	// Categories
	grocery, err := upsertCategoryByName(db, "Grocery", now)
	if err != nil {
		log.Fatalf("category grocery: %v", err)
	}
	beverages, err := upsertCategoryByName(db, "Beverages", now)
	if err != nil {
		log.Fatalf("category beverages: %v", err)
	}

	// Store
	store, err := upsertStoreByName(db, "Dev Mart", "MG Road, Bengaluru", 12.9716, 77.5946, now)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	// Products with image URLs
	products := []productSeed{
		{"Toned Milk 1L", "Fresh toned milk, 1 litre", 6500, "Grocery", "https://images.unsplash.com/photo-1550583724-b2692b85b150?w=600", 50},
		{"Brown Bread", "Whole-wheat loaf, 400g", 4500, "Grocery", "https://images.unsplash.com/photo-1509440159596-0249088772ff?w=600", 40},
		{"Farm Eggs (6 pcs)", "Grade A chicken eggs, pack of 6", 7800, "Grocery", "https://images.unsplash.com/photo-1587486913049-53fc88980cfc?w=600", 60},
		{"Basmati Rice 1kg", "Aged long-grain basmati, 1 kg", 18500, "Grocery", "https://images.unsplash.com/photo-1586201375761-83865001e31c?w=600", 25},
		{"Cola 750ml", "Chilled cola, 750 ml", 5500, "Beverages", "https://images.unsplash.com/photo-1554866585-cd94860890b7?w=600", 80},
		{"Orange Juice 1L", "Freshly squeezed, no added sugar", 15000, "Beverages", "https://images.unsplash.com/photo-1613478223719-2ab802602423?w=600", 30},
	}
	categoryByName := map[string]uuid.UUID{"Grocery": grocery.ID, "Beverages": beverages.ID}
	for _, p := range products {
		categoryID := categoryByName[p.Category]
		product, err := upsertProductByName(db, store.ID, p.Name, p.Description, p.PricePaise, p.ImageURL, categoryID, now)
		if err != nil {
			log.Fatalf("product %s: %v", p.Name, err)
		}
		if err := upsertInventory(db, store.ID, product.ID, p.StockQty, now); err != nil {
			log.Fatalf("inventory %s: %v", p.Name, err)
		}
	}

	// Delivery -> store assignment
	if err := upsertAssignment(db, rider.ID, store.ID, now); err != nil {
		log.Fatalf("assignment: %v", err)
	}

	fmt.Println("Seed complete:")
	fmt.Printf("  admin:   %s (%s)\n", admin.ID, admin.Phone)
	fmt.Printf("  rider:   %s (%s)\n", rider.ID, rider.Phone)
	fmt.Printf("  store:   %s (%s)\n", store.ID, store.Name)
	fmt.Printf("  seeded %d products\n", len(products))
}

func upsertUserByPhone(db *gorm.DB, phone, role, name string, now time.Time) (*domain.User, error) {
	var user domain.User
	err := db.Where("phone = ?", phone).First(&user).Error
	if err == nil {
		updates := map[string]any{"role": role, "status": "active", "updated_at": now}
		if user.Name == nil {
			updates["name"] = name
		}
		if err := db.Model(&user).Updates(updates).Error; err != nil {
			return nil, err
		}
		return &user, nil
	}
	nameCopy := name
	user = domain.User{ID: uuid.New(), Phone: phone, Name: &nameCopy, Role: role, Status: "active", PhoneVerifiedAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func upsertCategoryByName(db *gorm.DB, name string, now time.Time) (*domain.Category, error) {
	var cat domain.Category
	err := db.Where("name = ?", name).First(&cat).Error
	if err == nil {
		return &cat, nil
	}
	cat = domain.Category{ID: uuid.New(), Name: name, CreatedAt: now}
	if err := db.Create(&cat).Error; err != nil {
		return nil, err
	}
	return &cat, nil
}

func upsertStoreByName(db *gorm.DB, name, address string, lat, lng float64, now time.Time) (*domain.Store, error) {
	var store domain.Store
	err := db.Where("name = ?", name).First(&store).Error
	if err == nil {
		return &store, nil
	}
	latCopy, lngCopy := lat, lng
	store = domain.Store{ID: uuid.New(), Name: name, Address: address, Latitude: &latCopy, Longitude: &lngCopy, IsActive: true, CreatedAt: now}
	if err := db.Create(&store).Error; err != nil {
		return nil, err
	}
	return &store, nil
}

func upsertProductByName(db *gorm.DB, storeID uuid.UUID, name, description string, price int, imageURL string, categoryID uuid.UUID, now time.Time) (*domain.Product, error) {
	var product domain.Product
	err := db.Where("name = ?", name).First(&product).Error
	if err == nil {
		return &product, nil
	}
	descCopy, imgCopy, catCopy := description, imageURL, categoryID
	product = domain.Product{ID: uuid.New(), StoreID: storeID, Name: name, Description: &descCopy, Price: price, ImageURL: &imgCopy, Images: []string{imageURL}, CategoryID: &catCopy, CreatedAt: now}
	if err := db.Create(&product).Error; err != nil {
		return nil, err
	}
	return &product, nil
}

func upsertInventory(db *gorm.DB, storeID, productID uuid.UUID, quantity int, now time.Time) error {
	inventory := domain.Inventory{ID: uuid.New(), StoreID: storeID, ProductID: productID, Quantity: quantity, UpdatedAt: now}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "store_id"}, {Name: "product_id"}},
		DoUpdates: clause.Assignments(map[string]any{"quantity": quantity, "updated_at": now}),
	}).Create(&inventory).Error
}

func upsertAssignment(db *gorm.DB, deliveryID, storeID uuid.UUID, now time.Time) error {
	var count int64
	if err := db.Model(&domain.DeliveryStoreAssignment{}).Where("delivery_id = ? AND store_id = ?", deliveryID, storeID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	assignment := domain.DeliveryStoreAssignment{ID: uuid.New(), DeliveryID: deliveryID, StoreID: storeID, CreatedAt: now}
	return db.Create(&assignment).Error
}
