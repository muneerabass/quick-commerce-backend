package dto

type StoreRequest struct {
	Name      string   `json:"name"`
	Address   string   `json:"address"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	IsActive  *bool    `json:"is_active"`
}

type StorePatchRequest struct {
	Name      *string  `json:"name"`
	Address   *string  `json:"address"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	IsActive  *bool    `json:"is_active"`
}

type ProductRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Price       int     `json:"price"`
	CategoryID  *string `json:"category_id"`
}

type ProductPatchRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Price       *int    `json:"price"`
	CategoryID  *string `json:"category_id"`
}

type InventoryRequest struct {
	Quantity int `json:"quantity"`
}

type AddCartItemRequest struct {
	StoreID   string `json:"store_id"`
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type UpdateCartItemRequest struct {
	Quantity int `json:"quantity"`
}

type CreateOrderRequest struct {
	AddressID string `json:"address_id"`
}

type DeliveryUserRequest struct {
	Phone string  `json:"phone"`
	Name  *string `json:"name"`
}
