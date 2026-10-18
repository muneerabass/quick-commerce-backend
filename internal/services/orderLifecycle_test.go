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

type lifecycleFixture struct {
	db        *gorm.DB
	userID    uuid.UUID
	riderID   uuid.UUID
	storeID   uuid.UUID
	productID uuid.UUID
	addressID uuid.UUID
	cart      *CartService
	orders    *OrderService
	admin     *AdminService
	catalog   repository.CatalogRepository
	commerce  repository.CommerceRepository
}

// newLifecycleFixture builds one served customer, one rider assigned to the
// store, and a product priced 1000 paise with the given stock.
func newLifecycleFixture(t *testing.T, stock int) *lifecycleFixture {
	t.Helper()
	db := commerceTestDB(t)
	now := time.Now().UTC()
	f := &lifecycleFixture{db: db, userID: uuid.New(), riderID: uuid.New()}
	for _, user := range []*domain.User{
		{ID: f.userID, Phone: "+919876540001", Role: "customer", Status: "active", CreatedAt: now, UpdatedAt: now},
		{ID: f.riderID, Phone: "+919876540002", Role: "delivery", Status: "active", CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	f.storeID = seedPincodeStore(t, db, "Lifecycle Store", true, "560001")
	seedStockedProduct(t, db, f.storeID, "Milk", stock)
	var product domain.Product
	if err := db.Where("store_id = ?", f.storeID).First(&product).Error; err != nil {
		t.Fatal(err)
	}
	f.productID = product.ID
	if err := db.Model(&domain.Product{}).Where("id = ?", product.ID).Update("price", 1000).Error; err != nil {
		t.Fatal(err)
	}
	address := &domain.UserAddress{ID: uuid.New(), UserID: f.userID, Label: "Home", RecipientName: "C", Phone: "+919876540001", AddressLine1: "1 Rd", City: "BLR", State: "KA", Pincode: "560001", IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(address).Error; err != nil {
		t.Fatal(err)
	}
	f.addressID = address.ID
	f.catalog = repository.NewCatalogRepository(db)
	f.commerce = repository.NewCommerceRepository(db)
	addresses := repository.NewAddressRepository(db)
	f.cart = NewCartService(db, f.catalog, f.commerce).WithAddresses(addresses)
	f.orders = NewOrderService(db, addresses, f.catalog, f.commerce)
	f.admin = NewAdminService(db, repository.NewUserRepository(db), f.catalog, f.commerce)
	if err := f.commerce.CreateAssignment(nil, &domain.DeliveryStoreAssignment{ID: uuid.New(), DeliveryID: f.riderID, StoreID: f.storeID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *lifecycleFixture) addToCart(t *testing.T, quantity int) dto.CartResponse {
	t.Helper()
	response, err := f.cart.AddItem(f.userID, dto.AddCartItemRequest{StoreID: f.storeID.String(), ProductID: f.productID.String(), Quantity: quantity})
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func (f *lifecycleFixture) stock(t *testing.T) int {
	t.Helper()
	var inventory domain.Inventory
	if err := f.db.Where("store_id = ? AND product_id = ?", f.storeID, f.productID).First(&inventory).Error; err != nil {
		t.Fatal(err)
	}
	return inventory.Quantity
}

func (f *lifecycleFixture) place(t *testing.T, key string) dto.OrderResponse {
	t.Helper()
	order, err := f.orders.Create(f.userID, f.addressID, key)
	if err != nil {
		t.Fatalf("place order: %v", err)
	}
	return order
}

func TestFeesAreComputedOnTheServerAndStoredOnTheOrder(t *testing.T) {
	f := newLifecycleFixture(t, 100)
	small := f.addToCart(t, 1) // 1000 paise, below the free-delivery threshold
	if small.SubtotalAmount != 1000 || small.DeliveryFee != 2500 || small.HandlingFee != 400 || small.TotalAmount != 3900 {
		t.Fatalf("unexpected small cart totals: %+v", small)
	}
	if small.AmountToFreeDelivery != 19900-1000 {
		t.Fatalf("amount to free delivery wrong: %d", small.AmountToFreeDelivery)
	}
	big := f.addToCart(t, 19) // 20 x 1000 = 20000, over the threshold
	if big.DeliveryFee != 0 || big.TotalAmount != 20000+400 || big.AmountToFreeDelivery != 0 {
		t.Fatalf("free delivery not applied: %+v", big)
	}
	order := f.place(t, "")
	if order.SubtotalAmount != 20000 || order.DeliveryFee != 0 || order.HandlingFee != 400 || order.TotalAmount != 20400 || order.AmountToCollect != 20400 {
		t.Fatalf("order must store the same fees the cart showed: %+v", order)
	}
	if order.DeliveryCode == "" || len(order.DeliveryCode) != 4 {
		t.Fatalf("customer should receive a 4-digit delivery code, got %q", order.DeliveryCode)
	}
}

func TestQuantityLimits(t *testing.T) {
	f := newLifecycleFixture(t, 100)
	if _, err := f.cart.AddItem(f.userID, dto.AddCartItemRequest{StoreID: f.storeID.String(), ProductID: f.productID.String(), Quantity: 21}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("21 in one add should be rejected, got %v", err)
	}
	f.addToCart(t, 20)
	if _, err := f.cart.AddItem(f.userID, dto.AddCartItemRequest{StoreID: f.storeID.String(), ProductID: f.productID.String(), Quantity: 1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("going over 20 in the cart should be rejected, got %v", err)
	}
}

func TestIdempotentCheckoutCreatesOneOrder(t *testing.T) {
	f := newLifecycleFixture(t, 10)
	f.addToCart(t, 2)
	first := f.place(t, "key-1")
	second, err := f.orders.Create(f.userID, f.addressID, "key-1") // client retry after a timeout
	if err != nil {
		t.Fatalf("retry with the same key must return the original order, got %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("retry created a second order: %s vs %s", first.ID, second.ID)
	}
	if got := f.stock(t); got != 8 {
		t.Fatalf("stock should drop once, got %d", got)
	}
	var count int64
	f.db.Model(&domain.Order{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly one order, found %d", count)
	}
}

func TestCartIssuesBlockCheckoutAndLeaveStockUntouched(t *testing.T) {
	f := newLifecycleFixture(t, 5)
	f.addToCart(t, 3)

	// Stock drops below the cart quantity (another customer bought it).
	f.db.Model(&domain.Inventory{}).Where("store_id = ?", f.storeID).Update("quantity", 2)
	view, err := f.cart.Get(f.userID, &f.addressID)
	if err != nil || view.CanCheckout || len(view.Issues) != 1 || view.Issues[0].Code != issueInsufficient || *view.Issues[0].AvailableQuantity != 2 {
		t.Fatalf("expected insufficient_stock issue, got %+v err=%v", view, err)
	}
	if view.Items[0].AvailableQuantity != 2 {
		t.Fatalf("line should report available quantity, got %d", view.Items[0].AvailableQuantity)
	}
	var unavailable *CartUnavailableError
	if _, err := f.orders.Create(f.userID, f.addressID, ""); !errors.As(err, &unavailable) {
		t.Fatalf("expected CartUnavailableError, got %v", err)
	}
	if got := f.stock(t); got != 2 {
		t.Fatalf("a rejected order must not change stock, got %d", got)
	}

	// Sold out entirely.
	f.db.Model(&domain.Inventory{}).Where("store_id = ?", f.storeID).Update("quantity", 0)
	view, _ = f.cart.Get(f.userID, &f.addressID)
	if view.Issues[0].Code != issueOutOfStock {
		t.Fatalf("expected out_of_stock, got %+v", view.Issues)
	}

	// Store closes.
	f.db.Model(&domain.Inventory{}).Where("store_id = ?", f.storeID).Update("quantity", 5)
	f.db.Model(&domain.Store{}).Where("id = ?", f.storeID).Update("is_active", false)
	view, _ = f.cart.Get(f.userID, &f.addressID)
	if view.CanCheckout || !hasIssueCode(view.Issues, issueStoreClosed) {
		t.Fatalf("expected store_inactive, got %+v", view.Issues)
	}
	f.db.Model(&domain.Store{}).Where("id = ?", f.storeID).Update("is_active", true)

	// Address in an unserved pincode.
	other := &domain.UserAddress{ID: uuid.New(), UserID: f.userID, Label: "Work", RecipientName: "C", Phone: "+919876540001", AddressLine1: "2 Rd", City: "Delhi", State: "DL", Pincode: "110001", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	f.db.Create(other)
	view, _ = f.cart.Get(f.userID, &other.ID)
	if view.CanCheckout || !hasIssueCode(view.Issues, issueNotServed) {
		t.Fatalf("expected not_serviceable, got %+v", view.Issues)
	}
	if _, err := f.orders.Create(f.userID, other.ID, ""); !errors.Is(err, ErrNotServiceable) {
		t.Fatalf("expected ErrNotServiceable, got %v", err)
	}
	// Without an address the cart is otherwise fine.
	view, _ = f.cart.Get(f.userID, nil)
	if !view.CanCheckout {
		t.Fatalf("a healthy cart should be checkout-ready: %+v", view.Issues)
	}
}

func hasIssueCode(issues []dto.CartIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func TestCustomerCancelRestoresStockOnce(t *testing.T) {
	f := newLifecycleFixture(t, 10)
	f.addToCart(t, 4)
	order := f.place(t, "")
	if f.stock(t) != 6 {
		t.Fatalf("stock should be reserved at checkout, got %d", f.stock(t))
	}
	cancelled, err := f.orders.Cancel(f.userID, uuid.MustParse(order.ID), "changed my mind")
	if err != nil || cancelled.Status != "cancelled" || cancelled.CancelReason == nil || *cancelled.CancelReason != "changed my mind" || cancelled.AmountToCollect != 0 {
		t.Fatalf("expected a cancelled order, got %+v err=%v", cancelled, err)
	}
	if f.stock(t) != 10 {
		t.Fatalf("cancel must restore stock, got %d", f.stock(t))
	}
	if _, err := f.orders.Cancel(f.userID, uuid.MustParse(order.ID), ""); !errors.Is(err, ErrOrderNotCancellable) {
		t.Fatalf("second cancel should fail, got %v", err)
	}
	if f.stock(t) != 10 {
		t.Fatalf("a repeated cancel must not restore stock again, got %d", f.stock(t))
	}
	if len(cancelled.Events) < 2 || cancelled.Events[len(cancelled.Events)-1].Status != "cancelled" {
		t.Fatalf("history should end with the cancellation: %+v", cancelled.Events)
	}
	// Someone else's order looks like it doesn't exist.
	if _, err := f.orders.Cancel(uuid.New(), uuid.MustParse(order.ID), ""); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cancelling another user's order should be not found, got %v", err)
	}
}

func TestCancelAndAcceptAreMutuallyExclusive(t *testing.T) {
	f := newLifecycleFixture(t, 10)

	f.addToCart(t, 1)
	acceptedFirst := f.place(t, "")
	if _, err := f.orders.Accept(f.riderID, uuid.MustParse(acceptedFirst.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.orders.Cancel(f.userID, uuid.MustParse(acceptedFirst.ID), ""); !errors.Is(err, ErrOrderNotCancellable) {
		t.Fatalf("customer cannot cancel after a rider accepted, got %v", err)
	}
	if _, err := f.orders.Complete(f.riderID, uuid.MustParse(acceptedFirst.ID), acceptedFirst.DeliveryCode); err != nil {
		t.Fatal(err)
	}

	f.addToCart(t, 1)
	cancelledFirst := f.place(t, "")
	if _, err := f.orders.Cancel(f.userID, uuid.MustParse(cancelledFirst.ID), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.orders.Accept(f.riderID, uuid.MustParse(cancelledFirst.ID)); !errors.Is(err, repository.ErrOrderUnavailable) {
		t.Fatalf("a cancelled order cannot be accepted, got %v", err)
	}
}

func TestSweeperCancelsOnlyStalePendingOrders(t *testing.T) {
	f := newLifecycleFixture(t, 10)
	f.addToCart(t, 2)
	stale := f.place(t, "")
	f.db.Model(&domain.Order{}).Where("id = ?", stale.ID).Update("created_at", time.Now().UTC().Add(-2*time.Hour))
	f.addToCart(t, 1)
	fresh := f.place(t, "")
	f.addToCart(t, 1)
	accepted := f.place(t, "")
	f.db.Model(&domain.Order{}).Where("id = ?", accepted.ID).Update("created_at", time.Now().UTC().Add(-2*time.Hour))
	if _, err := f.orders.Accept(f.riderID, uuid.MustParse(accepted.ID)); err != nil {
		t.Fatal(err)
	}
	if f.stock(t) != 6 {
		t.Fatalf("setup: expected 6 left, got %d", f.stock(t))
	}

	n, err := f.orders.SweepStalePending(time.Now().UTC())
	if err != nil || n != 1 {
		t.Fatalf("expected exactly one stale order swept, got %d err=%v", n, err)
	}
	if f.stock(t) != 8 {
		t.Fatalf("the swept order's 2 units should return to stock, got %d", f.stock(t))
	}
	status := func(id string) string {
		var o domain.Order
		f.db.First(&o, "id = ?", id)
		return o.Status
	}
	if status(stale.ID) != "cancelled" || status(fresh.ID) != "pending" || status(accepted.ID) != "accepted" {
		t.Fatalf("sweep touched the wrong orders: stale=%s fresh=%s accepted=%s", status(stale.ID), status(fresh.ID), status(accepted.ID))
	}
	// A second pass finds nothing.
	if n, _ := f.orders.SweepStalePending(time.Now().UTC()); n != 0 {
		t.Fatalf("second sweep should be a no-op, got %d", n)
	}
}

func TestDeliveryCodeGuardsCompletion(t *testing.T) {
	f := newLifecycleFixture(t, 10)
	f.addToCart(t, 1)
	order := f.place(t, "")
	id := uuid.MustParse(order.ID)
	if _, err := f.orders.Accept(f.riderID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.orders.Complete(f.riderID, id, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an empty code is a validation error, got %v", err)
	}
	wrong := "0000"
	if order.DeliveryCode == wrong {
		wrong = "1111"
	}
	var badCode *InvalidDeliveryCodeError
	if _, err := f.orders.Complete(f.riderID, id, wrong); !errors.As(err, &badCode) || badCode.Remaining != 4 {
		t.Fatalf("expected 4 tries left, got %v", err)
	}
	done, err := f.orders.Complete(f.riderID, id, order.DeliveryCode)
	if err != nil || done.Status != "completed" || done.PaymentStatus != "paid" || done.DeliveryCode != "" {
		t.Fatalf("right code should complete without leaking the code: %+v err=%v", done, err)
	}
	// A retry after a timeout returns the finished order instead of an error.
	again, err := f.orders.Complete(f.riderID, id, order.DeliveryCode)
	if err != nil || again.Status != "completed" {
		t.Fatalf("repeating complete should be idempotent, got %+v err=%v", again, err)
	}
	// Another rider cannot complete it.
	if _, err := f.orders.Complete(uuid.New(), id, order.DeliveryCode); !errors.Is(err, repository.ErrOrderUnavailable) {
		t.Fatalf("a different rider must be refused, got %v", err)
	}
}

func TestDeliveryCodeLocksAfterTooManyAttempts(t *testing.T) {
	f := newLifecycleFixture(t, 10)
	f.addToCart(t, 1)
	order := f.place(t, "")
	id := uuid.MustParse(order.ID)
	if _, err := f.orders.Accept(f.riderID, id); err != nil {
		t.Fatal(err)
	}
	wrong := "0000"
	if order.DeliveryCode == wrong {
		wrong = "1111"
	}
	for i := 0; i < 4; i++ {
		f.orders.Complete(f.riderID, id, wrong)
	}
	if _, err := f.orders.Complete(f.riderID, id, wrong); !errors.Is(err, ErrDeliveryCodeLocked) {
		t.Fatalf("the fifth wrong code should lock completion, got %v", err)
	}
	if _, err := f.orders.Complete(f.riderID, id, order.DeliveryCode); !errors.Is(err, ErrDeliveryCodeLocked) {
		t.Fatalf("even the right code is refused once locked, got %v", err)
	}
}

func TestAdminCancelReassignAndQueue(t *testing.T) {
	f := newLifecycleFixture(t, 10)
	admin := uuid.New()

	f.addToCart(t, 2)
	order := f.place(t, "")
	id := uuid.MustParse(order.ID)
	if _, err := f.orders.Accept(f.riderID, id); err != nil {
		t.Fatal(err)
	}
	f.db.Model(&domain.Order{}).Where("id = ?", id).Update("accepted_at", time.Now().UTC().Add(-3*time.Hour))

	cutoff := time.Now().UTC().Add(-StaleAcceptedAfter())
	stale, err := f.orders.AdminList(repository.OrderFilter{AcceptedBefore: &cutoff})
	if err != nil || len(stale) != 1 || stale[0].ID != order.ID {
		t.Fatalf("the stale accepted order should be in the admin queue: %+v err=%v", stale, err)
	}

	back, err := f.orders.AdminReassign(admin, id)
	if err != nil || back.Status != "pending" || back.AcceptedBy != nil {
		t.Fatalf("reassign should return the order to pending: %+v err=%v", back, err)
	}
	if _, err := f.orders.AdminReassign(admin, id); !errors.Is(err, ErrOrderNotCancellable) {
		t.Fatalf("reassigning a pending order should fail, got %v", err)
	}
	if _, err := f.orders.AdminCancel(admin, id, "  "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("admin cancel needs a reason, got %v", err)
	}
	cancelled, err := f.orders.AdminCancel(admin, id, "rider unreachable")
	if err != nil || cancelled.Status != "cancelled" || *cancelled.CancelledByRole != "admin" {
		t.Fatalf("admin cancel failed: %+v err=%v", cancelled, err)
	}
	if f.stock(t) != 10 {
		t.Fatalf("admin cancel should restore stock, got %d", f.stock(t))
	}
}

func TestOpenOrdersBlockAccountDeletionAndRiderRemoval(t *testing.T) {
	f := newLifecycleFixture(t, 10)
	f.addToCart(t, 1)
	order := f.place(t, "")
	id := uuid.MustParse(order.ID)

	auth := testAuthService(f.db, &usefulRecordingSender{}, time.Minute, 0, 5)
	if err := auth.DeleteAccount(f.userID); !errors.Is(err, ErrActiveOrders) {
		t.Fatalf("a customer with a pending order cannot delete their account, got %v", err)
	}
	if _, err := f.orders.Accept(f.riderID, id); err != nil {
		t.Fatal(err)
	}
	if err := auth.DeleteAccount(f.riderID); !errors.Is(err, ErrActiveOrders) {
		t.Fatalf("a rider mid-delivery cannot delete their account, got %v", err)
	}
	if err := f.admin.RemoveDeliveryAssignment(f.riderID, f.storeID); !errors.Is(err, ErrActiveOrders) {
		t.Fatalf("a rider holding an order cannot be unassigned, got %v", err)
	}
	if _, err := f.orders.Complete(f.riderID, id, order.DeliveryCode); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.RemoveDeliveryAssignment(f.riderID, f.storeID); err != nil {
		t.Fatalf("once the delivery is done the rider can be unassigned: %v", err)
	}
	if err := auth.DeleteAccount(f.userID); err != nil {
		t.Fatalf("with no open orders the account can be deleted: %v", err)
	}
}

func TestOrderListPagination(t *testing.T) {
	f := newLifecycleFixture(t, 50)
	for i := 0; i < 5; i++ {
		f.addToCart(t, 1)
		order := f.place(t, "")
		f.db.Model(&domain.Order{}).Where("id = ?", order.ID).Update("created_at", time.Now().UTC().Add(-time.Duration(i)*time.Hour))
	}
	page, err := f.orders.List(f.userID, 2, nil)
	if err != nil || len(page.Orders) != 2 || page.NextBefore == nil {
		t.Fatalf("first page should hold 2 and a cursor: %+v err=%v", page, err)
	}
	next, err := f.orders.List(f.userID, 2, page.NextBefore)
	if err != nil || len(next.Orders) != 2 || next.Orders[0].ID == page.Orders[0].ID {
		t.Fatalf("second page should continue: %+v err=%v", next, err)
	}
	last, err := f.orders.List(f.userID, 2, next.NextBefore)
	if err != nil || len(last.Orders) != 1 || last.NextBefore != nil {
		t.Fatalf("last page should hold 1 and no cursor: %+v err=%v", last, err)
	}
}
