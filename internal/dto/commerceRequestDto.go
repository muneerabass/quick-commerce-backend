package dto

type StoreRequest struct {
	Name     string   `json:"name"`
	Address  string   `json:"address"`
	Pincodes []string `json:"pincodes"`
	IsActive *bool    `json:"is_active"`
}

type StorePatchRequest struct {
	Name     *string   `json:"name"`
	Address  *string   `json:"address"`
	Pincodes *[]string `json:"pincodes"`
	IsActive *bool     `json:"is_active"`
}

type ProductRequest struct {
	StoreID     string   `json:"store_id"`
	Name        string   `json:"name"`
	Description *string  `json:"description"`
	Price       int      `json:"price"`
	ImageURL    *string  `json:"image_url"`
	Images      []string `json:"images"`
	CategoryID  *string  `json:"category_id"`
	// Quantity is the initial stock set for this product at its store.
	Quantity *int `json:"quantity"`
}

type ProductPatchRequest struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Price       *int      `json:"price"`
	ImageURL    *string   `json:"image_url"`
	Images      *[]string `json:"images"`
	CategoryID  *string   `json:"category_id"`
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
	Phone   string  `json:"phone"`
	Name    *string `json:"name"`
	// StoreID, when set, assigns the new rider to that store in the same
	// transaction so a partial failure cannot leave an unassigned user (H-004).
	StoreID *string `json:"store_id"`
}

type CompleteOrderRequest struct {
	DeliveryCode string `json:"delivery_code"`
}

type CancelOrderRequest struct {
	Reason string `json:"reason"`
}
