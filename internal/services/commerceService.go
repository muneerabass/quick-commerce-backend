package services

import (
	"errors"
	"fmt"
	"time"

	"quick-commerce/internal/domain"
	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CartService struct {
	db       *gorm.DB
	catalog  repository.CatalogRepository
	commerce repository.CommerceRepository
}

func NewCartService(db *gorm.DB, catalog repository.CatalogRepository, commerce repository.CommerceRepository) *CartService {
	return &CartService{db: db, catalog: catalog, commerce: commerce}
}

func (s *CartService) Get(userID uuid.UUID) (dto.CartResponse, error) {
	cart, err := s.getOrCreateCart(userID)
	if err != nil {
		return dto.CartResponse{}, err
	}
	return s.mapCart(cart)
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
	if _, err := s.catalog.FindStore(nil, storeID); err != nil {
		return dto.CartResponse{}, err
	}
	if _, err := s.catalog.FindProduct(nil, productID); err != nil {
		return dto.CartResponse{}, err
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
		if inventory.Quantity < newQuantity {
			return ErrInsufficientStock
		}
		if itemErr == nil {
			item.Quantity = newQuantity
			item.UpdatedAt = time.Now().UTC()
			return s.commerce.UpdateCartItem(tx, item)
		}
		return s.commerce.CreateCartItem(tx, &domain.CartItem{ID: uuid.New(), CartID: cart.ID, ProductID: productID, Quantity: newQuantity, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	})
	if err != nil {
		return dto.CartResponse{}, err
	}
	return s.mapCart(cart)
}

func (s *CartService) UpdateItem(userID, itemID uuid.UUID, quantity int) (dto.CartResponse, error) {
	if quantity <= 0 {
		return dto.CartResponse{}, fmt.Errorf("%w: quantity must be greater than zero", ErrInvalidInput)
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
	return s.mapCart(cart)
}

func (s *CartService) DeleteItem(userID, itemID uuid.UUID) (dto.CartResponse, error) {
	cart, err := s.getOrCreateCart(userID)
	if err != nil {
		return dto.CartResponse{}, err
	}
	if err := s.commerce.DeleteCartItem(nil, cart.ID, itemID); err != nil {
		return dto.CartResponse{}, err
	}
	if items, err := s.commerce.ListCartItems(nil, cart.ID); err == nil && len(items) == 0 {
		cart.StoreID = nil
		cart.UpdatedAt = time.Now().UTC()
		if err := s.commerce.UpdateCart(nil, cart); err != nil {
			return dto.CartResponse{}, err
		}
	}
	return s.mapCart(cart)
}

func (s *CartService) Clear(userID uuid.UUID) error {
	cart, err := s.getOrCreateCart(userID)
	if err != nil {
		return err
	}
	if err := s.commerce.ClearCart(nil, cart.ID); err != nil {
		return err
	}
	cart.StoreID = nil
	cart.UpdatedAt = time.Now().UTC()
	return s.commerce.UpdateCart(nil, cart)
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

func (s *CartService) mapCart(cart *domain.Cart) (dto.CartResponse, error) {
	items, err := s.commerce.ListCartItems(nil, cart.ID)
	if err != nil {
		return dto.CartResponse{}, err
	}
	response := dto.CartResponse{ID: cart.ID.String(), Items: make([]dto.CartItemResponse, 0, len(items))}
	if cart.StoreID != nil {
		storeID := cart.StoreID.String()
		response.StoreID = &storeID
	}
	for _, item := range items {
		product, err := s.catalog.FindProduct(nil, item.ProductID)
		if err != nil {
			return dto.CartResponse{}, err
		}
		lineTotal := product.Price * item.Quantity
		response.TotalAmount += lineTotal
		response.Items = append(response.Items, dto.CartItemResponse{ID: item.ID.String(), ProductID: product.ID.String(), ProductName: product.Name, UnitPrice: product.Price, Quantity: item.Quantity, LineTotal: lineTotal})
	}
	return response, nil
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

func (s *OrderService) Create(userID, addressID uuid.UUID) (dto.OrderResponse, error) {
	address, err := s.addresses.Find(nil, userID, addressID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	var order *domain.Order
	err = s.db.Transaction(func(tx *gorm.DB) error {
		cart, err := s.commerce.FindCart(tx, userID, true)
		if err != nil {
			return err
		}
		if cart.StoreID == nil {
			return ErrEmptyCart
		}
		items, err := s.commerce.ListCartItems(tx, cart.ID)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return ErrEmptyCart
		}
		store, err := s.catalog.FindStore(tx, *cart.StoreID)
		if err != nil {
			return err
		}
		if !store.IsActive {
			return fmt.Errorf("%w: store is inactive", ErrInvalidInput)
		}
		order = &domain.Order{ID: uuid.New(), UserID: userID, StoreID: *cart.StoreID, Status: "pending", PaymentMethod: "cod", PaymentStatus: "pending", RecipientName: address.RecipientName, DeliveryPhone: address.Phone, AddressLine1: address.AddressLine1, AddressLine2: address.AddressLine2, Landmark: address.Landmark, City: address.City, State: address.State, Pincode: address.Pincode, Latitude: address.Latitude, Longitude: address.Longitude, DeliveryInstructions: address.DeliveryInstructions, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		orderItems := make([]domain.OrderItem, 0, len(items))
		for _, cartItem := range items {
			product, err := s.catalog.FindProduct(tx, cartItem.ProductID)
			if err != nil {
				return err
			}
			inventory, err := s.catalog.FindInventoryForUpdate(tx, *cart.StoreID, cartItem.ProductID)
			if err != nil {
				return err
			}
			if inventory.Quantity < cartItem.Quantity {
				return ErrInsufficientStock
			}
			inventory.Quantity -= cartItem.Quantity
			inventory.UpdatedAt = time.Now().UTC()
			if err := s.catalog.UpdateInventory(tx, inventory); err != nil {
				return err
			}
			lineTotal := product.Price * cartItem.Quantity
			order.TotalAmount += lineTotal
			orderItems = append(orderItems, domain.OrderItem{ID: uuid.New(), ProductID: product.ID, ProductName: product.Name, UnitPrice: product.Price, Quantity: cartItem.Quantity, LineTotal: lineTotal})
		}
		if err := s.commerce.CreateOrder(tx, order, orderItems); err != nil {
			return err
		}
		if err := s.commerce.ClearCart(tx, cart.ID); err != nil {
			return err
		}
		cart.StoreID = nil
		cart.UpdatedAt = time.Now().UTC()
		return s.commerce.UpdateCart(tx, cart)
	})
	if err != nil {
		return dto.OrderResponse{}, err
	}
	return s.mapOrder(order)
}

func (s *OrderService) List(userID uuid.UUID) ([]dto.OrderResponse, error) {
	orders, err := s.commerce.ListOrdersForUser(nil, userID)
	if err != nil {
		return nil, err
	}
	return s.mapOrders(orders)
}

func (s *OrderService) Get(userID, orderID uuid.UUID) (dto.OrderResponse, error) {
	order, err := s.commerce.FindOrderForUser(nil, userID, orderID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	return s.mapOrder(order)
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
		return s.mapOrder(order)
	}
	if order.Status == "pending" {
		assigned, err := s.commerce.IsAssigned(nil, deliveryID, order.StoreID)
		if err != nil {
			return dto.OrderResponse{}, err
		}
		if assigned {
			return s.mapOrder(order)
		}
	}
	return dto.OrderResponse{}, repository.ErrNotFound
}

func (s *OrderService) Accept(deliveryID, orderID uuid.UUID) (dto.OrderResponse, error) {
	now := time.Now().UTC()
	if err := s.commerce.AcceptOrder(nil, orderID, deliveryID, now); err != nil {
		return dto.OrderResponse{}, err
	}
	order, err := s.findOrder(orderID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	return s.mapOrder(order)
}

func (s *OrderService) Complete(deliveryID, orderID uuid.UUID) (dto.OrderResponse, error) {
	if err := s.commerce.CompleteOrder(nil, orderID, deliveryID, time.Now().UTC()); err != nil {
		return dto.OrderResponse{}, err
	}
	order, err := s.findOrder(orderID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	return s.mapOrder(order)
}

func (s *OrderService) findOrder(orderID uuid.UUID) (*domain.Order, error) {
	var order domain.Order
	if err := s.db.Where("id = ?", orderID).First(&order).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return &order, nil
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

func (s *OrderService) mapOrder(order *domain.Order) (dto.OrderResponse, error) {
	items, err := s.commerce.ListOrderItems(nil, order.ID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	response := dto.OrderResponse{ID: order.ID.String(), StoreID: order.StoreID.String(), Status: order.Status, PaymentMethod: order.PaymentMethod, PaymentStatus: order.PaymentStatus, TotalAmount: order.TotalAmount, RecipientName: order.RecipientName, DeliveryPhone: order.DeliveryPhone, AddressLine1: order.AddressLine1, AddressLine2: order.AddressLine2, Landmark: order.Landmark, City: order.City, State: order.State, Pincode: order.Pincode, Latitude: order.Latitude, Longitude: order.Longitude, AcceptedAt: order.AcceptedAt, CompletedAt: order.CompletedAt, CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt, Items: make([]dto.OrderItemResponse, 0, len(items))}
	if order.AcceptedBy != nil {
		value := order.AcceptedBy.String()
		response.AcceptedBy = &value
	}
	for _, item := range items {
		response.Items = append(response.Items, dto.OrderItemResponse{ProductID: item.ProductID.String(), ProductName: item.ProductName, UnitPrice: item.UnitPrice, Quantity: item.Quantity, LineTotal: item.LineTotal})
	}
	return response, nil
}
