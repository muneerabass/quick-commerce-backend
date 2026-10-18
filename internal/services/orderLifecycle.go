package services

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"quick-commerce/internal/domain"
	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OrderPolicy holds the tunable business rules for carts and orders. Amounts
// are in paise. Set it once at startup with SetOrderPolicy.
type OrderPolicy struct {
	DeliveryFee           int
	HandlingFee           int
	FreeDeliveryThreshold int
	// AcceptTimeout is how long a pending order waits for a rider before it is
	// cancelled automatically.
	AcceptTimeout time.Duration
	// StaleAcceptedAfter is how long a rider may hold an accepted order before
	// it shows in the admin "needs attention" queue.
	StaleAcceptedAfter time.Duration
	MaxLineQuantity    int
	MaxCartLines       int
	MaxCodeAttempts    int
}

func DefaultOrderPolicy() OrderPolicy {
	return OrderPolicy{
		DeliveryFee: 2500, HandlingFee: 400, FreeDeliveryThreshold: 19900,
		AcceptTimeout: 30 * time.Minute, StaleAcceptedAfter: 60 * time.Minute,
		MaxLineQuantity: 20, MaxCartLines: 50, MaxCodeAttempts: 5,
	}
}

var orderPolicy = DefaultOrderPolicy()

// SetOrderPolicy overrides the defaults. A negative fee keeps its default and a
// non-positive duration keeps its default.
func SetOrderPolicy(p OrderPolicy) {
	d := DefaultOrderPolicy()
	if p.DeliveryFee >= 0 {
		d.DeliveryFee = p.DeliveryFee
	}
	if p.HandlingFee >= 0 {
		d.HandlingFee = p.HandlingFee
	}
	if p.FreeDeliveryThreshold >= 0 {
		d.FreeDeliveryThreshold = p.FreeDeliveryThreshold
	}
	if p.AcceptTimeout > 0 {
		d.AcceptTimeout = p.AcceptTimeout
	}
	if p.StaleAcceptedAfter > 0 {
		d.StaleAcceptedAfter = p.StaleAcceptedAfter
	}
	orderPolicy = d
}

// StaleAcceptedAfter exposes the admin-queue threshold to handlers.
func StaleAcceptedAfter() time.Duration { return orderPolicy.StaleAcceptedAfter }

// fees returns the delivery and handling fee for a subtotal. An empty cart pays
// nothing; delivery is free from the threshold upwards.
func (p OrderPolicy) fees(subtotal int) (delivery, handling int) {
	if subtotal <= 0 {
		return 0, 0
	}
	if subtotal < p.FreeDeliveryThreshold {
		delivery = p.DeliveryFee
	}
	return delivery, p.HandlingFee
}

var (
	ErrOrderNotCancellable = errors.New("this order can no longer be cancelled")
	ErrDeliveryCodeLocked  = errors.New("too many wrong delivery codes; ask support to complete this order")
	ErrActiveOrders        = errors.New("finish or cancel your open orders first")
)

// InvalidDeliveryCodeError reports a wrong code and how many tries are left.
type InvalidDeliveryCodeError struct{ Remaining int }

func (e *InvalidDeliveryCodeError) Error() string {
	return fmt.Sprintf("that delivery code is incorrect (%d tries left)", e.Remaining)
}

// CartUnavailableError means the cart cannot be fulfilled as it stands.
type CartUnavailableError struct{ Issues []dto.CartIssue }

func (e *CartUnavailableError) Error() string {
	if len(e.Issues) > 0 {
		return e.Issues[0].Message
	}
	return "some items in your cart are no longer available"
}

const (
	issueOutOfStock   = "out_of_stock"
	issueInsufficient = "insufficient_stock"
	issueStoreClosed  = "store_inactive"
	issueNotServed    = "not_serviceable"
)

// cartView is a cart priced and checked against live stock, store state and
// (when an address is given) serviceability. Both the cart preview and order
// creation use it, so they cannot disagree.
type cartView struct {
	Response dto.CartResponse
	Items    []domain.CartItem
	Products []*domain.Product
	Store    *domain.Store
}

func (v cartView) hasIssue(code string) bool {
	for _, issue := range v.Response.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func buildCartView(db *gorm.DB, catalog repository.CatalogRepository, commerce repository.CommerceRepository, cart *domain.Cart, address *domain.UserAddress) (cartView, error) {
	items, err := commerce.ListCartItems(db, cart.ID)
	if err != nil {
		return cartView{}, err
	}
	view := cartView{Items: items}
	view.Response = dto.CartResponse{ID: cart.ID.String(), Items: make([]dto.CartItemResponse, 0, len(items)), Issues: []dto.CartIssue{}}
	if cart.StoreID != nil {
		store, err := catalog.FindStore(db, *cart.StoreID)
		if err != nil {
			return cartView{}, err
		}
		view.Store = store
		id, name := store.ID.String(), store.Name
		view.Response.StoreID, view.Response.StoreName = &id, &name
	}
	subtotal := 0
	for _, item := range items {
		product, err := catalog.FindProduct(db, item.ProductID)
		if err != nil {
			return cartView{}, err
		}
		view.Products = append(view.Products, product)
		available := 0
		if cart.StoreID != nil {
			inventory, err := catalog.FindInventoryForUpdate(db, *cart.StoreID, item.ProductID)
			if err != nil && !errors.Is(err, repository.ErrNotFound) {
				return cartView{}, err
			}
			if err == nil {
				available = inventory.Quantity
			}
		}
		lineTotal := product.Price * item.Quantity
		subtotal += lineTotal
		itemID := item.ID.String()
		view.Response.Items = append(view.Response.Items, dto.CartItemResponse{
			ID: itemID, ProductID: product.ID.String(), ProductName: product.Name, ImageURL: product.ImageURL,
			UnitPrice: product.Price, Quantity: item.Quantity, LineTotal: lineTotal, AvailableQuantity: available,
		})
		switch {
		case available <= 0:
			view.Response.Issues = append(view.Response.Issues, dto.CartIssue{Code: issueOutOfStock, ItemID: &itemID, Message: product.Name + " is out of stock"})
		case available < item.Quantity:
			left := available
			view.Response.Issues = append(view.Response.Issues, dto.CartIssue{Code: issueInsufficient, ItemID: &itemID, AvailableQuantity: &left,
				Message: fmt.Sprintf("Only %d of %s left", available, product.Name)})
		}
	}
	if view.Store != nil && !view.Store.IsActive {
		view.Response.Issues = append(view.Response.Issues, dto.CartIssue{Code: issueStoreClosed, Message: view.Store.Name + " is currently closed"})
	}
	if view.Store != nil && address != nil {
		serves, err := catalog.StoreServesPincode(db, view.Store.ID, address.Pincode)
		if err != nil {
			return cartView{}, err
		}
		if !serves {
			view.Response.Issues = append(view.Response.Issues, dto.CartIssue{Code: issueNotServed,
				Message: fmt.Sprintf("%s doesn't deliver to %s", view.Store.Name, address.Pincode)})
		}
	}
	delivery, handling := orderPolicy.fees(subtotal)
	r := &view.Response
	r.SubtotalAmount, r.DeliveryFee, r.HandlingFee = subtotal, delivery, handling
	r.TotalAmount = subtotal + delivery + handling
	r.FreeDeliveryThreshold = orderPolicy.FreeDeliveryThreshold
	if subtotal > 0 && subtotal < orderPolicy.FreeDeliveryThreshold {
		r.AmountToFreeDelivery = orderPolicy.FreeDeliveryThreshold - subtotal
	}
	r.CanCheckout = len(items) > 0 && len(r.Issues) == 0
	return view, nil
}

func generateDeliveryCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04d", n.Int64()), nil
}

func (s *OrderService) recordEvent(tx *gorm.DB, orderID uuid.UUID, status string, actorID *uuid.UUID, role, note string) error {
	event := &domain.OrderEvent{OrderID: orderID, Status: status, ActorID: actorID, ActorRole: role, CreatedAt: time.Now().UTC()}
	if note != "" {
		event.Note = &note
	}
	return s.commerce.AppendOrderEvent(tx, event)
}

// cancelOrder is the single place an order is cancelled: status flip, stock
// restore and history entry succeed or fail together.
func (s *OrderService) cancelOrder(orderID uuid.UUID, onlyUser *uuid.UUID, allowed []string, actorID *uuid.UUID, role, reason string) error {
	now := time.Now().UTC()
	return s.db.Transaction(func(tx *gorm.DB) error {
		err := s.commerce.CancelOrder(tx, orderID, onlyUser, allowed, repository.OrderCancellation{ActorID: actorID, Role: role, Reason: reason, Now: now})
		if errors.Is(err, repository.ErrOrderUnavailable) {
			// Tell "not yours / doesn't exist" apart from "too late".
			order, findErr := s.commerce.FindOrderByID(tx, orderID)
			if findErr != nil || (onlyUser != nil && order.UserID != *onlyUser) {
				return repository.ErrNotFound
			}
			return ErrOrderNotCancellable
		}
		if err != nil {
			return err
		}
		if err := s.commerce.RestoreInventory(tx, orderID, now); err != nil {
			return err
		}
		return s.recordEvent(tx, orderID, "cancelled", actorID, role, reason)
	})
}

// Cancel lets a customer cancel their own order until a rider accepts it.
func (s *OrderService) Cancel(userID, orderID uuid.UUID, reason string) (dto.OrderResponse, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "Cancelled by customer"
	}
	if len(reason) > 300 {
		return dto.OrderResponse{}, fmt.Errorf("%w: reason is too long", ErrInvalidInput)
	}
	if err := s.cancelOrder(orderID, &userID, []string{"pending"}, &userID, "customer", reason); err != nil {
		return dto.OrderResponse{}, err
	}
	return s.Get(userID, orderID)
}

// AdminList returns orders for the admin queue.
func (s *OrderService) AdminList(filter repository.OrderFilter) ([]dto.OrderResponse, error) {
	orders, err := s.commerce.ListOrdersAdmin(nil, filter)
	if err != nil {
		return nil, err
	}
	return s.mapOrders(orders)
}

// AdminCancel force-cancels a pending or accepted order (e.g. an unreachable rider).
func (s *OrderService) AdminCancel(adminID, orderID uuid.UUID, reason string) (dto.OrderResponse, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return dto.OrderResponse{}, fmt.Errorf("%w: a reason is required", ErrInvalidInput)
	}
	if len(reason) > 300 {
		return dto.OrderResponse{}, fmt.Errorf("%w: reason is too long", ErrInvalidInput)
	}
	if err := s.cancelOrder(orderID, nil, []string{"pending", "accepted"}, &adminID, "admin", reason); err != nil {
		return dto.OrderResponse{}, err
	}
	return s.adminOrder(orderID)
}

// AdminReassign puts an accepted order back in the pending pool for another rider.
func (s *OrderService) AdminReassign(adminID, orderID uuid.UUID) (dto.OrderResponse, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.commerce.ReassignOrder(tx, orderID, time.Now().UTC()); err != nil {
			return err
		}
		return s.recordEvent(tx, orderID, "pending", &adminID, "admin", "Reassigned by admin")
	})
	if errors.Is(err, repository.ErrOrderUnavailable) {
		if _, findErr := s.commerce.FindOrderByID(nil, orderID); findErr != nil {
			return dto.OrderResponse{}, repository.ErrNotFound
		}
		return dto.OrderResponse{}, ErrOrderNotCancellable
	}
	if err != nil {
		return dto.OrderResponse{}, err
	}
	return s.adminOrder(orderID)
}

func (s *OrderService) adminOrder(orderID uuid.UUID) (dto.OrderResponse, error) {
	order, err := s.commerce.FindOrderByID(nil, orderID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	return s.mapOrderDetail(order, viewerAdmin)
}

// SweepStalePending cancels pending orders no rider took within the accept
// timeout. It is safe to run on several instances at once.
func (s *OrderService) SweepStalePending(now time.Time) (int, error) {
	cutoff := now.Add(-orderPolicy.AcceptTimeout)
	cancelled := 0
	err := s.db.Transaction(func(tx *gorm.DB) error {
		ids, err := s.commerce.ListStalePendingOrderIDs(tx, cutoff, 50)
		if err != nil {
			return err
		}
		for _, id := range ids {
			err := s.commerce.CancelOrder(tx, id, nil, []string{"pending"}, repository.OrderCancellation{
				Role: "system", Reason: "No rider accepted your order in time", Now: now})
			if errors.Is(err, repository.ErrOrderUnavailable) {
				continue
			}
			if err != nil {
				return err
			}
			if err := s.commerce.RestoreInventory(tx, id, now); err != nil {
				return err
			}
			if err := s.recordEvent(tx, id, "cancelled", nil, "system", "No rider accepted your order in time"); err != nil {
				return err
			}
			cancelled++
		}
		return nil
	})
	return cancelled, err
}

// StartOrderSweeper runs SweepStalePending every interval until ctx ends.
func StartOrderSweeper(ctx context.Context, orders *OrderService, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if n, err := orders.SweepStalePending(now.UTC()); err != nil {
					log.Printf("order sweeper: %v", err)
				} else if n > 0 {
					log.Printf("order sweeper: cancelled %d stale pending order(s)", n)
				}
			}
		}
	}()
}

// verifyDeliveryCode checks the customer's code, counting wrong tries.
func (s *OrderService) verifyDeliveryCode(order *domain.Order, code string) error {
	if order.DeliveryCode == "" { // legacy order placed before codes existed
		return nil
	}
	max := orderPolicy.MaxCodeAttempts
	if order.DeliveryCodeAttempts >= max {
		return ErrDeliveryCodeLocked
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("%w: enter the delivery code the customer shows you", ErrInvalidInput)
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(order.DeliveryCode)) == 1 {
		return nil
	}
	attempts, err := s.commerce.RecordWrongDeliveryCode(nil, order.ID)
	if err != nil {
		return err
	}
	if attempts >= max {
		return ErrDeliveryCodeLocked
	}
	return &InvalidDeliveryCodeError{Remaining: max - attempts}
}

type orderViewer int

const (
	viewerCustomer orderViewer = iota
	viewerRider
	viewerAdmin
)

// mapOrderDetail is mapOrder plus the history, rider and (for the owner) the delivery code.
func (s *OrderService) mapOrderDetail(order *domain.Order, viewer orderViewer) (dto.OrderResponse, error) {
	response, err := s.mapOrder(order)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	events, err := s.commerce.ListOrderEvents(nil, order.ID)
	if err != nil {
		return dto.OrderResponse{}, err
	}
	for _, event := range events {
		response.Events = append(response.Events, dto.OrderEventResponse{Status: event.Status, ActorRole: event.ActorRole, Note: event.Note, CreatedAt: event.CreatedAt})
	}
	if viewer != viewerRider && order.AcceptedBy != nil && order.Status == "accepted" {
		var rider struct {
			Name  *string
			Phone string
		}
		if err := s.db.Table("users").Select("name, phone").Where("id = ?", *order.AcceptedBy).Scan(&rider).Error; err == nil && rider.Phone != "" {
			response.Rider = &dto.RiderInfo{Name: rider.Name, Phone: rider.Phone}
		}
	}
	if viewer == viewerCustomer && (order.Status == "pending" || order.Status == "accepted") {
		response.DeliveryCode = order.DeliveryCode
	}
	return response, nil
}
