package services

import (
	"errors"
	"testing"
	"time"

	"quick-commerce/internal/domain"
	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func commerceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testDB(t)
	if err := db.AutoMigrate(&domain.Store{}, &domain.Category{}, &domain.Product{}, &domain.Inventory{}, &domain.Cart{}, &domain.CartItem{}, &domain.Order{}, &domain.OrderItem{}, &domain.DeliveryStoreAssignment{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCatalogCartAndCODOrderFlow(t *testing.T) {
	db := commerceTestDB(t)
	userID := uuid.New()
	deliveryID := uuid.New()
	storeID := uuid.New()
	productID := uuid.New()
	now := time.Now().UTC()
	if err := db.Create(&domain.User{ID: userID, Phone: "+919876543230", Role: "customer", Status: "active", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.User{ID: deliveryID, Phone: "+919876543231", Role: "delivery", Status: "active", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	latitude, longitude := 19.076, 72.8777
	if err := db.Create(&domain.Store{ID: storeID, Name: "Central Store", Address: "Mumbai", Latitude: &latitude, Longitude: &longitude, IsActive: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Product{ID: productID, Name: "Milk", Price: 50, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Inventory{ID: uuid.New(), StoreID: storeID, ProductID: productID, Quantity: 3, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	address := &domain.UserAddress{ID: uuid.New(), UserID: userID, Label: "Home", RecipientName: "Customer", Phone: "+919876543230", AddressLine1: "1 Main Road", City: "Mumbai", State: "MH", Pincode: "400001", IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(address).Error; err != nil {
		t.Fatal(err)
	}

	catalogRepo := repository.NewCatalogRepository(db)
	commerceRepo := repository.NewCommerceRepository(db)
	catalog := NewCatalogService(catalogRepo)
	nearby, err := catalog.Nearby(latitude, longitude, 1)
	if err != nil || len(nearby) != 1 || len(nearby[0].Products) != 1 {
		t.Fatalf("expected nearby product, response=%+v err=%v", nearby, err)
	}

	cart := NewCartService(db, catalogRepo, commerceRepo)
	cartResponse, err := cart.AddItem(userID, dto.AddCartItemRequest{StoreID: storeID.String(), ProductID: productID.String(), Quantity: 2})
	if err != nil || cartResponse.TotalAmount != 100 {
		t.Fatalf("expected cart total, response=%+v err=%v", cartResponse, err)
	}
	orderService := NewOrderService(db, repository.NewAddressRepository(db), catalogRepo, commerceRepo)
	order, err := orderService.Create(userID, address.ID)
	if err != nil || order.Status != "pending" || order.PaymentMethod != "cod" || order.TotalAmount != 100 {
		t.Fatalf("expected pending COD order, order=%+v err=%v", order, err)
	}
	var inventory domain.Inventory
	if err := db.Where("store_id = ? AND product_id = ?", storeID, productID).First(&inventory).Error; err != nil {
		t.Fatal(err)
	}
	if inventory.Quantity != 1 {
		t.Fatalf("expected stock decrement, quantity=%d", inventory.Quantity)
	}

	if err := commerceRepo.CreateAssignment(nil, &domain.DeliveryStoreAssignment{ID: uuid.New(), DeliveryID: deliveryID, StoreID: storeID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	pending, err := orderService.PendingForDelivery(deliveryID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected delivery order, orders=%+v err=%v", pending, err)
	}
	accepted, err := orderService.Accept(deliveryID, uuid.MustParse(order.ID))
	if err != nil || accepted.Status != "accepted" {
		t.Fatalf("expected accepted order, order=%+v err=%v", accepted, err)
	}
	completed, err := orderService.Complete(deliveryID, uuid.MustParse(order.ID))
	if err != nil || completed.Status != "completed" || completed.PaymentStatus != "paid" {
		t.Fatalf("expected completed COD order, order=%+v err=%v", completed, err)
	}
}

func TestCartCannotMixStoresAndOrderAcceptIsSingleWinner(t *testing.T) {
	db := commerceTestDB(t)
	userID := uuid.New()
	firstDeliveryID := uuid.New()
	secondDeliveryID := uuid.New()
	storeID := uuid.New()
	productID := uuid.New()
	now := time.Now().UTC()
	for _, user := range []*domain.User{
		{ID: userID, Phone: "+919876543232", Role: "customer", Status: "active", CreatedAt: now, UpdatedAt: now},
		{ID: firstDeliveryID, Phone: "+919876543233", Role: "delivery", Status: "active", CreatedAt: now, UpdatedAt: now},
		{ID: secondDeliveryID, Phone: "+919876543234", Role: "delivery", Status: "active", CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	firstLat, firstLon := 12.9, 77.6
	secondStoreID := uuid.New()
	secondLat, secondLon := 12.91, 77.61
	for _, store := range []*domain.Store{
		{ID: storeID, Name: "One", Address: "A", Latitude: &firstLat, Longitude: &firstLon, IsActive: true, CreatedAt: now},
		{ID: secondStoreID, Name: "Two", Address: "B", Latitude: &secondLat, Longitude: &secondLon, IsActive: true, CreatedAt: now},
	} {
		if err := db.Create(store).Error; err != nil {
			t.Fatal(err)
		}
	}
	secondProductID := uuid.New()
	for _, product := range []*domain.Product{
		{ID: productID, Name: "A", Price: 10, CreatedAt: now},
		{ID: secondProductID, Name: "B", Price: 20, CreatedAt: now},
	} {
		if err := db.Create(product).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, inventory := range []*domain.Inventory{
		{ID: uuid.New(), StoreID: storeID, ProductID: productID, Quantity: 2, UpdatedAt: now},
		{ID: uuid.New(), StoreID: secondStoreID, ProductID: secondProductID, Quantity: 2, UpdatedAt: now},
	} {
		if err := db.Create(inventory).Error; err != nil {
			t.Fatal(err)
		}
	}
	address := &domain.UserAddress{ID: uuid.New(), UserID: userID, Label: "Home", RecipientName: "Customer", Phone: "+919876543232", AddressLine1: "1 Main Road", City: "Bengaluru", State: "KA", Pincode: "560001", IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(address).Error; err != nil {
		t.Fatal(err)
	}
	catalogRepo := repository.NewCatalogRepository(db)
	commerceRepo := repository.NewCommerceRepository(db)
	cart := NewCartService(db, catalogRepo, commerceRepo)
	if _, err := cart.AddItem(userID, dto.AddCartItemRequest{StoreID: storeID.String(), ProductID: productID.String(), Quantity: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := cart.AddItem(userID, dto.AddCartItemRequest{StoreID: secondStoreID.String(), ProductID: secondProductID.String(), Quantity: 1}); !errors.Is(err, ErrCartStoreMismatch) {
		t.Fatalf("expected one-store cart restriction, got %v", err)
	}
	orderService := NewOrderService(db, repository.NewAddressRepository(db), catalogRepo, commerceRepo)
	order, err := orderService.Create(userID, address.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := commerceRepo.CreateAssignment(nil, &domain.DeliveryStoreAssignment{ID: uuid.New(), DeliveryID: firstDeliveryID, StoreID: storeID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := commerceRepo.CreateAssignment(nil, &domain.DeliveryStoreAssignment{ID: uuid.New(), DeliveryID: secondDeliveryID, StoreID: storeID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := orderService.Accept(firstDeliveryID, uuid.MustParse(order.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := orderService.Accept(secondDeliveryID, uuid.MustParse(order.ID)); !errors.Is(err, repository.ErrOrderUnavailable) {
		t.Fatalf("expected second acceptance to fail, got %v", err)
	}
}
