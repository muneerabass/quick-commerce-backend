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
	if err := db.AutoMigrate(&domain.Store{}, &domain.StorePincode{}, &domain.Category{}, &domain.Product{}, &domain.Inventory{}, &domain.Cart{}, &domain.CartItem{}, &domain.Order{}, &domain.OrderItem{}, &domain.OrderEvent{}, &domain.DeliveryStoreAssignment{}); err != nil {
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
	if err := db.Create(&domain.Store{ID: storeID, Name: "Central Store", Address: "Mumbai", IsActive: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.StorePincode{StoreID: storeID, Pincode: "400001", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Product{ID: productID, StoreID: storeID, Name: "Milk", Price: 50, CreatedAt: now}).Error; err != nil {
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
	home, err := catalog.ProductsForPincode("400001")
	if err != nil || !home.Serviceable || len(home.Products) != 1 || home.Products[0].StoreName != "Central Store" {
		t.Fatalf("expected pincode product labelled with its store, response=%+v err=%v", home, err)
	}

	cart := NewCartService(db, catalogRepo, commerceRepo)
	cartResponse, err := cart.AddItem(userID, dto.AddCartItemRequest{StoreID: storeID.String(), ProductID: productID.String(), Quantity: 2})
	if err != nil || cartResponse.SubtotalAmount != 100 || cartResponse.TotalAmount != 100+2500+400 {
		t.Fatalf("expected cart total, response=%+v err=%v", cartResponse, err)
	}
	orderService := NewOrderService(db, repository.NewAddressRepository(db), catalogRepo, commerceRepo)
	order, err := orderService.Create(userID, address.ID, "")
	if err != nil || order.Status != "pending" || order.PaymentMethod != "cod" || order.TotalAmount != 3000 || order.SubtotalAmount != 100 {
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
	completed, err := orderService.Complete(deliveryID, uuid.MustParse(order.ID), order.DeliveryCode)
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
	secondStoreID := uuid.New()
	for _, store := range []*domain.Store{
		{ID: storeID, Name: "One", Address: "A", IsActive: true, CreatedAt: now},
		{ID: secondStoreID, Name: "Two", Address: "B", IsActive: true, CreatedAt: now},
	} {
		if err := db.Create(store).Error; err != nil {
			t.Fatal(err)
		}
	}
	secondProductID := uuid.New()
	for _, product := range []*domain.Product{
		{ID: productID, StoreID: storeID, Name: "A", Price: 10, CreatedAt: now},
		{ID: secondProductID, StoreID: secondStoreID, Name: "B", Price: 20, CreatedAt: now},
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
	if err := db.Create(&domain.StorePincode{StoreID: storeID, Pincode: "560001", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
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
	order, err := orderService.Create(userID, address.ID, "")
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

func TestRiderCanHoldOnlyOneActiveOrder(t *testing.T) {
	db := commerceTestDB(t)
	now := time.Now().UTC()
	userID := uuid.New()
	riderID := uuid.New()
	storeID := uuid.New()
	for _, user := range []*domain.User{
		{ID: userID, Phone: "+919876543240", Role: "customer", Status: "active", CreatedAt: now, UpdatedAt: now},
		{ID: riderID, Phone: "+919876543241", Role: "delivery", Status: "active", CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&domain.Store{ID: storeID, Name: "One", Address: "A", IsActive: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	commerceRepo := repository.NewCommerceRepository(db)
	catalogRepo := repository.NewCatalogRepository(db)
	orderService := NewOrderService(db, repository.NewAddressRepository(db), catalogRepo, commerceRepo)
	if err := commerceRepo.CreateAssignment(nil, &domain.DeliveryStoreAssignment{ID: uuid.New(), DeliveryID: riderID, StoreID: storeID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	// Two independent pending orders at the rider's store.
	orderA := uuid.New()
	orderB := uuid.New()
	for _, id := range []uuid.UUID{orderA, orderB} {
		o := &domain.Order{ID: id, UserID: userID, StoreID: storeID, Status: "pending", PaymentMethod: "cod", PaymentStatus: "pending", TotalAmount: 100, RecipientName: "C", DeliveryPhone: "+919876543240", AddressLine1: "1 Main", City: "BLR", State: "KA", Pincode: "560001", CreatedAt: now, UpdatedAt: now}
		if err := db.Create(o).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := orderService.Accept(riderID, orderA); err != nil {
		t.Fatalf("first accept should succeed: %v", err)
	}
	// Second accept while A is still active must be refused.
	if _, err := orderService.Accept(riderID, orderB); !errors.Is(err, repository.ErrRiderBusy) {
		t.Fatalf("expected ErrRiderBusy, got %v", err)
	}
	// The rider's active list should show exactly order A.
	active, err := orderService.MineForDelivery(riderID, "accepted")
	if err != nil || len(active) != 1 || active[0].ID != orderA.String() {
		t.Fatalf("expected one active order A, got %+v err=%v", active, err)
	}
	// After completing A, the rider can accept B.
	if _, err := orderService.Complete(riderID, orderA, ""); err != nil {
		t.Fatalf("complete A: %v", err)
	}
	if _, err := orderService.Accept(riderID, orderB); err != nil {
		t.Fatalf("accept B after completing A should succeed: %v", err)
	}
	history, err := orderService.MineForDelivery(riderID, "completed")
	if err != nil || len(history) != 1 || history[0].ID != orderA.String() {
		t.Fatalf("expected A in completed history, got %+v err=%v", history, err)
	}
}

func seedPincodeStore(t *testing.T, db *gorm.DB, name string, active bool, pincodes ...string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	if err := db.Create(&domain.Store{ID: id, Name: name, Address: name + " address", IsActive: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if !active {
		if err := db.Model(&domain.Store{}).Where("id = ?", id).Update("is_active", false).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, pincode := range pincodes {
		if err := db.Create(&domain.StorePincode{StoreID: id, Pincode: pincode, CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func seedStockedProduct(t *testing.T, db *gorm.DB, storeID uuid.UUID, name string, quantity int) {
	t.Helper()
	now := time.Now().UTC()
	product := domain.Product{ID: uuid.New(), StoreID: storeID, Name: name, Price: 10, CreatedAt: now}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Inventory{ID: uuid.New(), StoreID: storeID, ProductID: product.ID, Quantity: quantity, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestPincodeCatalogIsUnionOfServingStores(t *testing.T) {
	db := commerceTestDB(t)
	a := seedPincodeStore(t, db, "Store A", true, "560001", "560002")
	b := seedPincodeStore(t, db, "Store B", true, "560001")
	c := seedPincodeStore(t, db, "Store C", true, "560001", "560003")
	inactive := seedPincodeStore(t, db, "Store Closed", false, "560001")
	far := seedPincodeStore(t, db, "Store Far", true, "700001")
	seedStockedProduct(t, db, a, "Apples", 5)
	seedStockedProduct(t, db, b, "Bread", 5)
	seedStockedProduct(t, db, c, "Curd", 5)
	seedStockedProduct(t, db, c, "Out of stock", 0)
	seedStockedProduct(t, db, inactive, "Hidden", 5)
	seedStockedProduct(t, db, far, "Faraway", 5)

	catalog := NewCatalogService(repository.NewCatalogRepository(db))
	home, err := catalog.ProductsForPincode(" 560001 ")
	if err != nil {
		t.Fatal(err)
	}
	if !home.Serviceable || len(home.Stores) != 3 {
		t.Fatalf("expected 3 serving stores, got %+v", home.Stores)
	}
	names := map[string]string{}
	for _, product := range home.Products {
		names[product.Name] = product.StoreName
	}
	want := map[string]string{"Apples": "Store A", "Bread": "Store B", "Curd": "Store C"}
	if len(names) != len(want) {
		t.Fatalf("expected union %v, got %v", want, names)
	}
	for name, store := range want {
		if names[name] != store {
			t.Fatalf("product %s should be labelled %s, got %q", name, store, names[name])
		}
	}

	only, err := catalog.ProductsForPincode("560002")
	if err != nil || len(only.Products) != 1 || only.Products[0].Name != "Apples" {
		t.Fatalf("expected only Store A product for 560002, got %+v err=%v", only, err)
	}
}

func TestPincodeCatalogStates(t *testing.T) {
	db := commerceTestDB(t)
	empty := seedPincodeStore(t, db, "Empty Store", true, "560010")
	_ = empty
	catalog := NewCatalogService(repository.NewCatalogRepository(db))

	notServed, err := catalog.ProductsForPincode("999999")
	if err != nil || notServed.Serviceable || len(notServed.Products) != 0 || notServed.Products == nil {
		t.Fatalf("unserved pincode should be a non-error empty response, got %+v err=%v", notServed, err)
	}
	served, err := catalog.ProductsForPincode("560010")
	if err != nil || !served.Serviceable || len(served.Products) != 0 {
		t.Fatalf("served pincode without stock should be serviceable and empty, got %+v err=%v", served, err)
	}
	for _, bad := range []string{"", "abc", "12"} {
		if _, err := catalog.ProductsForPincode(bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("pincode %q should be invalid, got %v", bad, err)
		}
	}
}

func TestOrderRejectedWhenStoreDoesNotServeAddressPincode(t *testing.T) {
	db := commerceTestDB(t)
	now := time.Now().UTC()
	userID := uuid.New()
	if err := db.Create(&domain.User{ID: userID, Phone: "+919876543250", Role: "customer", Status: "active", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	storeID := seedPincodeStore(t, db, "Local", true, "560001")
	seedStockedProduct(t, db, storeID, "Milk", 5)
	var product domain.Product
	if err := db.Where("store_id = ?", storeID).First(&product).Error; err != nil {
		t.Fatal(err)
	}
	address := &domain.UserAddress{ID: uuid.New(), UserID: userID, Label: "Home", RecipientName: "C", Phone: "+919876543250", AddressLine1: "1 Rd", City: "Delhi", State: "DL", Pincode: "110001", IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(address).Error; err != nil {
		t.Fatal(err)
	}
	catalogRepo := repository.NewCatalogRepository(db)
	commerceRepo := repository.NewCommerceRepository(db)
	cart := NewCartService(db, catalogRepo, commerceRepo)
	if _, err := cart.AddItem(userID, dto.AddCartItemRequest{StoreID: storeID.String(), ProductID: product.ID.String(), Quantity: 1}); err != nil {
		t.Fatal(err)
	}
	orders := NewOrderService(db, repository.NewAddressRepository(db), catalogRepo, commerceRepo)
	if _, err := orders.Create(userID, address.ID, ""); !errors.Is(err, ErrNotServiceable) {
		t.Fatalf("expected ErrNotServiceable, got %v", err)
	}
	var stock domain.Inventory
	if err := db.Where("store_id = ?", storeID).First(&stock).Error; err != nil || stock.Quantity != 5 {
		t.Fatalf("stock must be untouched by a rejected order, qty=%d err=%v", stock.Quantity, err)
	}
}

func TestAdminStorePincodes(t *testing.T) {
	db := commerceTestDB(t)
	admin := NewAdminService(db, repository.NewUserRepository(db), repository.NewCatalogRepository(db), repository.NewCommerceRepository(db))

	if _, err := admin.CreateStore(dto.StoreRequest{Name: "S", Address: "A"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a store with no pincodes must be rejected, got %v", err)
	}
	if _, err := admin.CreateStore(dto.StoreRequest{Name: "S", Address: "A", Pincodes: []string{"56"}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a malformed pincode must be rejected, got %v", err)
	}
	store, err := admin.CreateStore(dto.StoreRequest{Name: "S", Address: "A", Pincodes: []string{"560001", " 560001 ", "560002"}})
	if err != nil || len(store.Pincodes) != 2 {
		t.Fatalf("expected de-duplicated pincodes, got %+v err=%v", store, err)
	}
	id := uuid.MustParse(store.ID)
	replacement := []string{"110001"}
	updated, err := admin.UpdateStore(id, dto.StorePatchRequest{Pincodes: &replacement})
	if err != nil || len(updated.Pincodes) != 1 || updated.Pincodes[0] != "110001" {
		t.Fatalf("expected pincodes replaced, got %+v err=%v", updated, err)
	}
	name := "Renamed"
	renamed, err := admin.UpdateStore(id, dto.StorePatchRequest{Name: &name})
	if err != nil || len(renamed.Pincodes) != 1 {
		t.Fatalf("renaming must keep pincodes, got %+v err=%v", renamed, err)
	}
	empty := []string{}
	if _, err := admin.UpdateStore(id, dto.StorePatchRequest{Pincodes: &empty}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("clearing every pincode must be rejected, got %v", err)
	}
	listed, err := admin.ListStores()
	if err != nil || len(listed) != 1 || len(listed[0].Pincodes) != 1 {
		t.Fatalf("list should include pincodes, got %+v err=%v", listed, err)
	}
}
