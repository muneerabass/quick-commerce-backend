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

type CartService struct {
	db        *gorm.DB
	catalog   repository.CatalogRepository
	commerce  repository.CommerceRepository
	addresses repository.AddressRepository
}

func NewCartService(db *gorm.DB, catalog repository.CatalogRepository, commerce repository.CommerceRepository) *CartService {
	return &CartService{db: db, catalog: catalog, commerce: commerce}
}

// WithAddresses lets Get check a chosen address for serviceability.
func (s *CartService) WithAddresses(addresses repository.AddressRepository) *CartService {
	s.addresses = addresses
	return s
}

// Get returns the priced cart. With an address it also reports whether the cart's
// store delivers there, so the app can warn before checkout.
func (s *CartService) Get(userID uuid.UUID, addressID *uuid.UUID) (dto.CartResponse, error) {
	cart, err := s.getOrCreateCart(userID)
	if err != nil {
		return dto.CartResponse{}, err
	}
	var address *domain.UserAddress
	if addressID != nil && s.addresses != nil {
		address, err = s.addresses.Find(nil, userID, *addressID)
		if err != nil {
			return dto.CartResponse{}, err
		}
	}
	return s.mapCart(cart, address)
}

func (s *CartService) AddItem(userID uuid.UUID, request dto.AddCartItemRequest) (dto.CartResponse, error) {
	storeID, err := uuid.Parse(request.StoreID)
	if err != nil {
		return dto.CartResponse{}, fmt.Errorf("%w: invalid store id", ErrInvalidInput)
	}
	productID, err := uuid.Parse(request.ProductID)
	if err != nil {
		return dto.CartResponse{}, fmt.Errorf("%w: invalid product id", ErrInvalidInput)
	}
	if request.Quantity <= 0 {
		return dto.CartResponse{}, fmt.Errorf("%w: quantity must be greater than zero", ErrInvalidInput)
	}
	if request.Quantity > orderPolicy.MaxLineQuantity {
		return dto.CartResponse{}, fmt.Errorf("%w: you can add at most %d of one item", ErrInvalidInput, orderPolicy.MaxLineQuantity)
	}
	store, err := s.catalog.FindStore(nil, storeID)
	if err != nil {
		return dto.CartResponse{}, err
	}
	if !store.IsActive {
		return dto.CartResponse{}, fmt.Errorf("%w: this store is currently unavailable", ErrInvalidInput)
	}
	product, err := s.catalog.FindProduct(nil, productID)
	if err != nil {
		return dto.CartResponse{}, err
	}
	if product.StoreID != storeID {
		return dto.CartResponse{}, repository.ErrNotFound
	}
	var cart *domain.Cart
	err = s.db.Transaction(func(tx *gorm.DB) error {
		cart, err = s.findOrCreateCart(tx, userID)
		if err != nil {
			return err
		}
		if cart.StoreID != nil && *cart.StoreID != storeID {
			return ErrCartStoreMismatch
		}
		if cart.StoreID == nil {
			cart.StoreID = &storeID
			cart.UpdatedAt = time.Now().UTC()
			if err := s.commerce.UpdateCart(tx, cart); err != nil {
				return err
			}
		}
		inventory, err := s.catalog.FindInventoryForUpdate(tx, storeID, productID)
		if err != nil {
			return err
		}
		item, itemErr := s.commerce.FindCartItemByProduct(tx, cart.ID, productID, true)
		if itemErr != nil && !errors.Is(itemErr, repository.ErrNotFound) {
			return itemErr
		}
		newQuantity := request.Quantity
		if itemErr == nil {
			newQuantity += item.Quantity
		}
		if newQuantity > orderPolicy.MaxLineQuantity {
			return fmt.Errorf("%w: you can add at most %d of one item", ErrInvalidInput, orderPolicy.MaxLineQuantity)
		}
		if inventory.Quantity < newQuantity {
			return ErrInsufficientStock
		}
		if itemErr == nil {
			item.Quantity = newQuantity
			item.UpdatedAt = time.Now().UTC()
			return s.commerce.UpdateCartItem(tx, item)
		}
		lines, err := s.commerce.ListCartItems(tx, cart.ID)
		if err != nil {
			return err
		}
		if len(lines) >= orderPolicy.MaxCartLines {
			return fmt.Errorf("%w: a cart can hold at most %d different items", ErrInvalidInput, orderPolicy.MaxCartLines)
		}
		return s.commerce.CreateCartItem(tx, &domain.CartItem{ID: uuid.New(), CartID: cart.ID, ProductID: productID, Quantity: newQuantity, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	})
	if err != nil {
		return dto.CartResponse{}, err
	}
	return s.mapCart(cart, nil)
}

func (s *CartService) UpdateItem(userID, itemID uuid.UUID, quantity int) (dto.CartResponse, error) {
	if quantity <= 0 {
		return dto.CartResponse{}, fmt.Errorf("%w: quantity must be greater than zero", ErrInvalidInput)
	}
	if quantity > orderPolicy.MaxLineQuantity {
		return dto.CartResponse{}, fmt.Errorf("%w: you can add at most %d of one item", ErrInvalidInput, orderPolicy.MaxLineQuantity)
	}
	var cart *domain.Cart
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		cart, err = s.findOrCreateCart(tx, userID)
		if err != nil {
			return err
		}
		item, err := s.commerce.FindCartItem(tx, cart.ID, itemID)
		if err != nil {
			return err
		}
		if cart.StoreID == nil {
			return ErrInvalidInput
		}
		inventory, err := s.catalog.FindInventoryForUpdate(tx, *cart.StoreID, item.ProductID)
		if err != nil {
			return err
		}
		if inventory.Quantity < quantity {
			return ErrInsufficientStock
		}
		item.Quantity = quantity
		item.UpdatedAt = time.Now().UTC()
		return s.commerce.UpdateCartItem(tx, item)
	})
	if err != nil {
		return dto.CartResponse{}, err
	}
	return s.mapCart(cart, nil)
}

func (s *CartService) DeleteItem(userID, itemID uuid.UUID) (dto.CartResponse, error) {
	// M-010: lock the cart row so a concurrent delete cannot leave a stale
	// store_id when two requests both remove the last item simultaneously.
	var cart *domain.Cart
	err := s.db.Transaction(func(tx *gorm.DB) error {
		c, err := s.commerce.FindCart(tx, userID, true)
		if err != nil {
			return err
		}
		cart = c
		if err := s.commerce.DeleteCartItem(tx, cart.ID, itemID); err != nil {
			return err
		}
		items, err := s.commerce.ListCartItems(tx, cart.ID)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			cart.StoreID = nil
			cart.UpdatedAt = time.Now().UTC()
			if err := s.commerce.UpdateCart(tx, cart); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return dto.CartResponse{}, err
	}
	return s.mapCart(cart, nil)
}

func (s *CartService) Clear(userID uuid.UUID) error {
	// M-010: lock the cart row before clearing so concurrent deletes cannot
	// race to leave a stale store_id.
	return s.db.Transaction(func(tx *gorm.DB) error {
		cart, err := s.commerce.FindCart(tx, userID, true)
		if err != nil {
			return err
		}
		if err := s.commerce.ClearCart(tx, cart.ID); err != nil {
			return err
		}
		cart.StoreID = nil
		cart.UpdatedAt = time.Now().UTC()
		return s.commerce.UpdateCart(tx, cart)
	})
}

func (s *CartService) getOrCreateCart(userID uuid.UUID) (*domain.Cart, error) {
	return s.findOrCreateCart(nil, userID)
}

func (s *CartService) findOrCreateCart(db *gorm.DB, userID uuid.UUID) (*domain.Cart, error) {
	cart, err := s.commerce.FindCart(db, userID, db != nil)
	if err == nil {
		return cart, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	cart = &domain.Cart{ID: uuid.New(), UserID: userID, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := s.commerce.CreateCart(db, cart); err != nil {
		return nil, err
	}
	return cart, nil
}

func (s *CartService) mapCart(cart *domain.Cart, address *domain.UserAddress) (dto.CartResponse, error) {
	view, err := buildCartView(nil, s.catalog, s.commerce, cart, address)
	if err != nil {
		return dto.CartResponse{}, err
	}
	return view.Response, nil
}

type OrderService struct {
	db        *gorm.DB
	addresses repository.AddressRepository
	catalog   repository.CatalogRepository
	commerce  repository.CommerceRepository
}

func NewOrderService(db *gorm.DB, addresses repository.AddressRepository, catalog repository.CatalogRepository, commerce repository.CommerceRepository) *OrderService {
	return &OrderService{db: db, addresses: addresses, catalog: catalog, commerce: commerce}
}

// Create turns the cart into a COD order. idempotencyKey (optional) makes a
// retry of the same checkout return the original order instead of failing.
func (s *OrderService) Create(userID, addressID uuid.UUID, idempotencyKey string) (dto.OrderResponse, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if len(idempotencyKey) > 100 {
		return dto.OrderResponse{}, fmt.Errorf("%w: idempotency key is too long", ErrInvalidInput)
	}
	if existing, ok, err := s.existingForKey(userID, idempotencyKey); err != nil {
		return dto.OrderResponse{}, err
	} else if ok {
		return existing, nil
	}
	address, err := s.addresses.Find(nil, userID, addressID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	code, err := generateDeliveryCode()
	if err != nil {
		return dto.OrderResponse{}, err
	}
	var order *domain.Order
	err = s.db.Transaction(func(tx *gorm.DB) error {
		cart, err := s.commerce.FindCart(tx, userID, true)
		if err != nil {
			return err
		}
		// M-001: enforce the one-active-order rule inside the transaction so
		// the cart lock and the count see a consistent snapshot.
		if open, err := s.commerce.CountActiveOrdersForCustomer(tx, userID); err != nil {
			return err
		} else if open > 0 {
			return ErrActiveOrders
		}
		if cart.StoreID == nil {
			return ErrEmptyCart
		}
		view, err := buildCartView(tx, s.catalog, s.commerce, cart, address)
		if err != nil {
			return err
		}
		if len(view.Items) == 0 {
			return ErrEmptyCart
		}
		if view.hasIssue(issueNotServed) {
			return &NotServiceableError{Store: view.Store.Name, Pincode: address.Pincode}
		}
		if len(view.Response.Issues) > 0 {
			return &CartUnavailableError{Issues: view.Response.Issues}
		}
		now := time.Now().UTC()
		order = &domain.Order{
			ID: uuid.New(), UserID: userID, StoreID: *cart.StoreID, Status: "pending", PaymentMethod: "cod", PaymentStatus: "pending",
			SubtotalAmount: view.Response.SubtotalAmount, DeliveryFee: view.Response.DeliveryFee, HandlingFee: view.Response.HandlingFee,
			TotalAmount:   view.Response.TotalAmount,
			RecipientName: address.RecipientName, DeliveryPhone: address.Phone, AddressLine1: address.AddressLine1, AddressLine2: address.AddressLine2,
			Landmark: address.Landmark, City: address.City, State: address.State, Pincode: address.Pincode,
			Latitude: address.Latitude, Longitude: address.Longitude, DeliveryInstructions: address.DeliveryInstructions,
			DeliveryCode: code, CreatedAt: now, UpdatedAt: now,
		}
		if idempotencyKey != "" {
			order.IdempotencyKey = &idempotencyKey
		}
		orderItems := make([]domain.OrderItem, 0, len(view.Items))
		for i, cartItem := range view.Items {
			product := view.Products[i]
			// Rows were locked and checked in buildCartView; decrement them now.
			inventory, err := s.catalog.FindInventoryForUpdate(tx, *cart.StoreID, cartItem.ProductID)
			if err != nil {
				return err
			}
			if inventory.Quantity < cartItem.Quantity {
				return ErrInsufficientStock
			}
			inventory.Quantity -= cartItem.Quantity
			inventory.UpdatedAt = now
			if err := s.catalog.UpdateInventory(tx, inventory); err != nil {
				return err
			}
			orderItems = append(orderItems, domain.OrderItem{ID: uuid.New(), ProductID: product.ID, ProductName: product.Name, UnitPrice: product.Price, Quantity: cartItem.Quantity, LineTotal: product.Price * cartItem.Quantity})
		}
		if err := s.commerce.CreateOrder(tx, order, orderItems); err != nil {
			return err
		}
		if err := s.recordEvent(tx, order.ID, "pending", &userID, "customer", "Order placed"); err != nil {
			return err
		}
		if err := s.commerce.ClearCart(tx, cart.ID); err != nil {
			return err
		}
		cart.StoreID = nil
		cart.UpdatedAt = now
		return s.commerce.UpdateCart(tx, cart)
	})
	if err != nil {
		// A concurrent request with the same key may have won the race.
		if existing, ok, lookupErr := s.existingForKey(userID, idempotencyKey); lookupErr == nil && ok {
			return existing, nil
		}
		return dto.OrderResponse{}, err
	}
	return s.mapOrderDetail(order, viewerCustomer)
}

func (s *OrderService) existingForKey(userID uuid.UUID, key string) (dto.OrderResponse, bool, error) {
	if key == "" {
		return dto.OrderResponse{}, false, nil
	}
	existing, err := s.commerce.FindOrderByIdempotencyKey(nil, userID, key)
	if errors.Is(err, repository.ErrNotFound) {
		return dto.OrderResponse{}, false, nil
	}
	if err != nil {
		return dto.OrderResponse{}, false, err
	}
	response, err := s.mapOrderDetail(existing, viewerCustomer)
	return response, err == nil, err
}

// List returns a page of the customer's orders, newest first. Pass the previous
// page's NextBefore to continue.
func (s *OrderService) List(userID uuid.UUID, limit int, before *time.Time) (dto.OrderPage, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	orders, err := s.commerce.ListOrdersForUserPage(nil, userID, limit, before)
	if err != nil {
		return dto.OrderPage{}, err
	}
	mapped, err := s.mapOrders(orders)
	if err != nil {
		return dto.OrderPage{}, err
	}
	page := dto.OrderPage{Orders: mapped}
	if len(orders) == limit {
		next := orders[len(orders)-1].CreatedAt
		page.NextBefore = &next
	}
	return page, nil
}

func (s *OrderService) Get(userID, orderID uuid.UUID) (dto.OrderResponse, error) {
	order, err := s.commerce.FindOrderForUser(nil, userID, orderID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	return s.mapOrderDetail(order, viewerCustomer)
}

func (s *OrderService) PendingForDelivery(deliveryID uuid.UUID) ([]dto.OrderResponse, error) {
	orders, err := s.commerce.ListPendingOrdersForDelivery(nil, deliveryID)
	if err != nil {
		return nil, err
	}
	return s.mapOrders(orders)
}

// MineForDelivery lists a rider's own orders by status: "accepted" (their active
// delivery) or "completed" (their delivery history).
func (s *OrderService) MineForDelivery(deliveryID uuid.UUID, status string) ([]dto.OrderResponse, error) {
	if status != "accepted" && status != "completed" {
		return nil, fmt.Errorf("%w: status must be accepted or completed", ErrInvalidInput)
	}
	orders, err := s.commerce.ListOrdersForDeliveryBy(nil, deliveryID, status)
	if err != nil {
		return nil, err
	}
	return s.mapOrders(orders)
}

// GetForDelivery returns a single order visible to a rider: pending orders at
// any store they're assigned to, or any order already accepted by this rider.
func (s *OrderService) GetForDelivery(deliveryID, orderID uuid.UUID) (dto.OrderResponse, error) {
	order, err := s.findOrder(orderID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	if order.AcceptedBy != nil && *order.AcceptedBy == deliveryID {
		return s.mapOrderDetail(order, viewerRider)
	}
	if order.Status == "pending" {
		assigned, err := s.commerce.IsAssigned(nil, deliveryID, order.StoreID)
		if err != nil {
			return dto.OrderResponse{}, err
		}
		if assigned {
			return s.mapOrderDetail(order, viewerRider)
		}
	}
	return dto.OrderResponse{}, repository.ErrNotFound
}

func (s *OrderService) Accept(deliveryID, orderID uuid.UUID) (dto.OrderResponse, error) {
	now := time.Now().UTC()
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.commerce.AcceptOrder(tx, orderID, deliveryID, now); err != nil {
			return err
		}
		return s.recordEvent(tx, orderID, "accepted", &deliveryID, "delivery", "Rider accepted your order")
	})
	if err != nil {
		return dto.OrderResponse{}, err
	}
	order, err := s.findOrder(orderID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	return s.mapOrderDetail(order, viewerRider)
}

// Complete marks a COD order delivered. The rider must give the customer's
// delivery code; repeating the call on an already-completed order returns it.
func (s *OrderService) Complete(deliveryID, orderID uuid.UUID, code string) (dto.OrderResponse, error) {
	order, err := s.findOrder(orderID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	if order.Status == "completed" && order.AcceptedBy != nil && *order.AcceptedBy == deliveryID {
		return s.mapOrderDetail(order, viewerRider)
	}
	if order.Status != "accepted" || order.AcceptedBy == nil || *order.AcceptedBy != deliveryID {
		return dto.OrderResponse{}, repository.ErrOrderUnavailable
	}
	if err := s.verifyDeliveryCode(order, code); err != nil {
		return dto.OrderResponse{}, err
	}
	now := time.Now().UTC()
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.commerce.CompleteOrder(tx, orderID, deliveryID, now); err != nil {
			return err
		}
		return s.recordEvent(tx, orderID, "completed", &deliveryID, "delivery", "Delivered and cash collected")
	})
	if err != nil {
		return dto.OrderResponse{}, err
	}
	order, err = s.findOrder(orderID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	return s.mapOrderDetail(order, viewerRider)
}

func (s *OrderService) findOrder(orderID uuid.UUID) (*domain.Order, error) {
	return s.commerce.FindOrderByID(nil, orderID)
}

func (s *OrderService) mapOrders(orders []domain.Order) ([]dto.OrderResponse, error) {
	responses := make([]dto.OrderResponse, 0, len(orders))
	for i := range orders {
		mapped, err := s.mapOrder(&orders[i])
		if err != nil {
			return nil, err
		}
		responses = append(responses, mapped)
	}
	return responses, nil
}

// mapOrder is the light form used in lists: items and totals, no history, rider or code.
func (s *OrderService) mapOrder(order *domain.Order) (dto.OrderResponse, error) {
	items, err := s.commerce.ListOrderItems(nil, order.ID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	collect := 0
	if order.PaymentStatus == "pending" && (order.Status == "pending" || order.Status == "accepted") {
		collect = order.TotalAmount
	}
	response := dto.OrderResponse{
		ID: order.ID.String(), StoreID: order.StoreID.String(), Status: order.Status, PaymentMethod: order.PaymentMethod,
		PaymentStatus: order.PaymentStatus, TotalAmount: order.TotalAmount, RecipientName: order.RecipientName,
		DeliveryPhone: order.DeliveryPhone, AddressLine1: order.AddressLine1, AddressLine2: order.AddressLine2,
		Landmark: order.Landmark, City: order.City, State: order.State, Pincode: order.Pincode,
		Latitude: order.Latitude, Longitude: order.Longitude, AcceptedAt: order.AcceptedAt, CompletedAt: order.CompletedAt,
		CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt, Items: make([]dto.OrderItemResponse, 0, len(items)),
		SubtotalAmount: order.SubtotalAmount, DeliveryFee: order.DeliveryFee, HandlingFee: order.HandlingFee,
		AmountToCollect: collect, CancelledAt: order.CancelledAt, CancelReason: order.CancelReason, CancelledByRole: order.CancelledByRole,
	}
	if store, err := s.catalog.FindStore(nil, order.StoreID); err == nil {
		response.StoreName = store.Name
	}
	if order.AcceptedBy != nil {
		value := order.AcceptedBy.String()
		response.AcceptedBy = &value
	}
	for _, item := range items {
		response.Items = append(response.Items, dto.OrderItemResponse{ProductID: item.ProductID.String(), ProductName: item.ProductName, UnitPrice: item.UnitPrice, Quantity: item.Quantity, LineTotal: item.LineTotal})
	}
	return response, nil
}
