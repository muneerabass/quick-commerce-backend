package dto

type UserRequestDto struct {
	Name string `json:"name"`
}

type SendOTPRequest struct {
	Phone string `json:"phone"`
}

type VerifyOTPRequest struct {
	Phone string `json:"phone"`
	OTP   string `json:"otp"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type UpdateProfileRequest struct {
	Name *string `json:"name"`
}

type AddressRequest struct {
	Label                string   `json:"label"`
	RecipientName        string   `json:"recipient_name"`
	Phone                string   `json:"phone"`
	AddressLine1         string   `json:"address_line1"`
	AddressLine2         *string  `json:"address_line2"`
	Landmark             *string  `json:"landmark"`
	City                 string   `json:"city"`
	State                string   `json:"state"`
	Pincode              string   `json:"pincode"`
	Latitude             *float64 `json:"latitude"`
	Longitude            *float64 `json:"longitude"`
	DeliveryInstructions *string  `json:"delivery_instructions"`
	IsDefault            *bool    `json:"is_default"`
}

type AddressPatchRequest struct {
	Label                *string  `json:"label"`
	RecipientName        *string  `json:"recipient_name"`
	Phone                *string  `json:"phone"`
	AddressLine1         *string  `json:"address_line1"`
	AddressLine2         *string  `json:"address_line2"`
	Landmark             *string  `json:"landmark"`
	City                 *string  `json:"city"`
	State                *string  `json:"state"`
	Pincode              *string  `json:"pincode"`
	Latitude             *float64 `json:"latitude"`
	Longitude            *float64 `json:"longitude"`
	DeliveryInstructions *string  `json:"delivery_instructions"`
	IsDefault            *bool    `json:"is_default"`
}

type GeocodeRequest struct {
	Address string `json:"address"`
}
