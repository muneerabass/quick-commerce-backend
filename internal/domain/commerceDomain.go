package domain

import (
	"time"

	"github.com/google/uuid"
)

type Store struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Latitude  *float64  `json:"latitude,omitempty"`
	Longitude *float64  `json:"longitude,omitempty"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

type Category struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Product struct {
	ID          uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Price       int        `json:"price"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty" gorm:"type:uuid"`
	CreatedAt   time.Time  `json:"created_at"`
}

type ProductInventory struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Price       int        `json:"price"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	Quantity    int        `json:"quantity"`
}

type Inventory struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	StoreID   uuid.UUID `json:"store_id" gorm:"type:uuid;index"`
	ProductID uuid.UUID `json:"product_id" gorm:"type:uuid;index"`
	Quantity  int       `json:"quantity"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Inventory) TableName() string { return "inventory" }

type Cart struct {
	ID        uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey"`
	UserID    uuid.UUID  `json:"user_id" gorm:"type:uuid;uniqueIndex"`
	StoreID   *uuid.UUID `json:"store_id,omitempty" gorm:"type:uuid"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type CartItem struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	CartID    uuid.UUID `json:"cart_id" gorm:"type:uuid;index"`
	ProductID uuid.UUID `json:"product_id" gorm:"type:uuid;index"`
	Quantity  int       `json:"quantity"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Order struct {
	ID                   uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey"`
	UserID               uuid.UUID  `json:"user_id" gorm:"type:uuid;index"`
	StoreID              uuid.UUID  `json:"store_id" gorm:"type:uuid;index"`
	Status               string     `json:"status"`
	PaymentMethod        string     `json:"payment_method"`
	PaymentStatus        string     `json:"payment_status"`
	TotalAmount          int        `json:"total_amount"`
	RecipientName        string     `json:"recipient_name"`
	DeliveryPhone        string     `json:"delivery_phone"`
	AddressLine1         string     `json:"address_line1"`
	AddressLine2         *string    `json:"address_line2,omitempty"`
	Landmark             *string    `json:"landmark,omitempty"`
	City                 string     `json:"city"`
	State                string     `json:"state"`
	Pincode              string     `json:"pincode"`
	Latitude             *float64   `json:"latitude,omitempty"`
	Longitude            *float64   `json:"longitude,omitempty"`
	DeliveryInstructions *string    `json:"delivery_instructions,omitempty"`
	AcceptedBy           *uuid.UUID `json:"accepted_by,omitempty" gorm:"type:uuid"`
	AcceptedAt           *time.Time `json:"accepted_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type OrderItem struct {
	ID          uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	OrderID     uuid.UUID `json:"order_id" gorm:"type:uuid;index"`
	ProductID   uuid.UUID `json:"product_id" gorm:"type:uuid"`
	ProductName string    `json:"product_name"`
	UnitPrice   int       `json:"unit_price"`
	Quantity    int       `json:"quantity"`
	LineTotal   int       `json:"line_total"`
}

type DeliveryStoreAssignment struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	DeliveryID uuid.UUID `json:"delivery_id" gorm:"type:uuid;index"`
	StoreID    uuid.UUID `json:"store_id" gorm:"type:uuid;index"`
	CreatedAt  time.Time `json:"created_at"`
}
