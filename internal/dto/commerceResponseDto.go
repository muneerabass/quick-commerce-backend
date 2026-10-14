package dto

import "time"

type StoreResponse struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Address    string   `json:"address"`
	Latitude   *float64 `json:"latitude,omitempty"`
	Longitude  *float64 `json:"longitude,omitempty"`
	IsActive   bool     `json:"is_active"`
	DistanceKM float64  `json:"distance_km,omitempty"`
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
	Quantity    int      `json:"quantity"`
}

type NearbyStoreResponse struct {
	Store    StoreResponse            `json:"store"`
	Products []CatalogProductResponse `json:"products"`
}

type CartItemResponse struct {
	ID          string `json:"id"`
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	UnitPrice   int    `json:"unit_price"`
	Quantity    int    `json:"quantity"`
	LineTotal   int    `json:"line_total"`
}

type CartResponse struct {
	ID          string             `json:"id"`
	StoreID     *string            `json:"store_id,omitempty"`
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
