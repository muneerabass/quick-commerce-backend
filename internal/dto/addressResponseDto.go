package dto

import "time"

type AddressResponse struct {
	ID                   string    `json:"id"`
	Label                string    `json:"label"`
	RecipientName        string    `json:"recipient_name"`
	Phone                string    `json:"phone"`
	AddressLine1         string    `json:"address_line1"`
	AddressLine2         *string   `json:"address_line2,omitempty"`
	Landmark             *string   `json:"landmark,omitempty"`
	City                 string    `json:"city"`
	State                string    `json:"state"`
	Pincode              string    `json:"pincode"`
	Latitude             *float64  `json:"latitude,omitempty"`
	Longitude            *float64  `json:"longitude,omitempty"`
	DeliveryInstructions *string   `json:"delivery_instructions,omitempty"`
	IsDefault            bool      `json:"is_default"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}
