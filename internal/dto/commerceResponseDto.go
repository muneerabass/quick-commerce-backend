package dto

import "time"

type StoreResponse struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Address  string   `json:"address"`
	Pincodes []string `json:"pincodes"`
	IsActive bool     `json:"is_active"`
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
}

type CartResponse struct {
	ID          string             `json:"id"`
	StoreID     *string            `json:"store_id,omitempty"`
	StoreName   *string            `json:"store_name,omitempty"`
	Items       []CartItemResponse `json:"items"`
	TotalAmount int                `json:"total_amount"`
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
}
