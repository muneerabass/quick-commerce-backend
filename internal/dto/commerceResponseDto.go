package dto

import "time"

type StoreResponse struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Address  string   `json:"address"`
	Pincodes []string `json:"pincodes"`
	IsActive bool     `json:"is_active"`
	// OpenOrders is set when a store is deactivated: pending/accepted orders
	// still being served.
	OpenOrders int `json:"open_orders,omitempty"`
}

type CatalogProductResponse struct {
	ID          string   `json:"id"`
	StoreID     string   `json:"store_id,omitempty"`
	Name        string   `json:"name"`
	Description *string  `json:"description,omitempty"`
	Price       int      `json:"price"`
	ImageURL    *string  `json:"image_url,omitempty"`
	Images      []string `json:"images"`
	CategoryID  *string  `json:"category_id,omitempty"`
	// CategoryName and StoreName let clients label a product without extra
	// lookups; a pincode catalog mixes products from several stores.
	CategoryName *string `json:"category_name,omitempty"`
	StoreName    string  `json:"store_name"`
	Quantity     int     `json:"quantity"`
}

// PincodeCatalogResponse is the home-screen catalog for one pincode: the union
// of in-stock products across every active store serving it.
type PincodeCatalogResponse struct {
	Pincode     string                   `json:"pincode"`
	Serviceable bool                     `json:"serviceable"`
	Stores      []StoreSummary           `json:"stores"`
	Products    []CatalogProductResponse `json:"products"`
}

type StoreSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CartItemResponse struct {
	ID          string  `json:"id"`
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	ImageURL    *string `json:"image_url,omitempty"`
	UnitPrice   int     `json:"unit_price"`
	Quantity    int     `json:"quantity"`
	LineTotal   int     `json:"line_total"`
	// AvailableQuantity is the stock the store has right now; below Quantity
	// means the line cannot be fulfilled as-is.
	AvailableQuantity int `json:"available_quantity"`
}

// CartIssue explains why a cart cannot be checked out as it stands.
type CartIssue struct {
	Code              string  `json:"code"`
	ItemID            *string `json:"item_id,omitempty"`
	Message           string  `json:"message"`
	AvailableQuantity *int    `json:"available_quantity,omitempty"`
}

type CartResponse struct {
	ID        string             `json:"id"`
	StoreID   *string            `json:"store_id,omitempty"`
	StoreName *string            `json:"store_name,omitempty"`
	Items     []CartItemResponse `json:"items"`
	// SubtotalAmount is items only; TotalAmount is what the customer pays
	// (subtotal + delivery + handling). All amounts are in paise.
	SubtotalAmount        int         `json:"subtotal_amount"`
	DeliveryFee           int         `json:"delivery_fee"`
	HandlingFee           int         `json:"handling_fee"`
	TotalAmount           int         `json:"total_amount"`
	FreeDeliveryThreshold int         `json:"free_delivery_threshold"`
	AmountToFreeDelivery  int         `json:"amount_to_free_delivery"`
	Issues                []CartIssue `json:"issues"`
	CanCheckout           bool        `json:"can_checkout"`
}

type OrderItemResponse struct {
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	UnitPrice   int    `json:"unit_price"`
	Quantity    int    `json:"quantity"`
	LineTotal   int    `json:"line_total"`
}

type OrderResponse struct {
	ID            string              `json:"id"`
	StoreID       string              `json:"store_id"`
	StoreName     string              `json:"store_name,omitempty"`
	Status        string              `json:"status"`
	PaymentMethod string              `json:"payment_method"`
	PaymentStatus string              `json:"payment_status"`
	TotalAmount   int                 `json:"total_amount"`
	RecipientName string              `json:"recipient_name"`
	DeliveryPhone string              `json:"delivery_phone"`
	AddressLine1  string              `json:"address_line1"`
	AddressLine2  *string             `json:"address_line2,omitempty"`
	Landmark      *string             `json:"landmark,omitempty"`
	City          string              `json:"city"`
	State         string              `json:"state"`
	Pincode       string              `json:"pincode"`
	Latitude      *float64            `json:"latitude,omitempty"`
	Longitude     *float64            `json:"longitude,omitempty"`
	AcceptedBy    *string             `json:"accepted_by,omitempty"`
	AcceptedAt    *time.Time          `json:"accepted_at,omitempty"`
	CompletedAt   *time.Time          `json:"completed_at,omitempty"`
	Items         []OrderItemResponse `json:"items"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`

	SubtotalAmount int `json:"subtotal_amount"`
	DeliveryFee    int `json:"delivery_fee"`
	HandlingFee    int `json:"handling_fee"`
	// AmountToCollect is the cash the rider takes at the door (0 once settled).
	AmountToCollect int        `json:"amount_to_collect"`
	CancelledAt     *time.Time `json:"cancelled_at,omitempty"`
	CancelReason    *string    `json:"cancel_reason,omitempty"`
	CancelledByRole *string    `json:"cancelled_by_role,omitempty"`
	// DeliveryCode is only sent to the customer who owns the order, while it is open.
	DeliveryCode string               `json:"delivery_code,omitempty"`
	Rider        *RiderInfo           `json:"rider,omitempty"`
	Events       []OrderEventResponse `json:"events,omitempty"`
}

type RiderInfo struct {
	Name  *string `json:"name,omitempty"`
	Phone string  `json:"phone"`
}

type OrderEventResponse struct {
	Status    string    `json:"status"`
	ActorRole string    `json:"actor_role"`
	Note      *string   `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// OrderPage is one page of a customer's orders; pass NextBefore as ?before= for the next.
type OrderPage struct {
	Orders     []OrderResponse `json:"orders"`
	NextBefore *time.Time      `json:"next_before,omitempty"`
}
