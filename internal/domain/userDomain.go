package domain

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID              uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey"`
	Phone           string     `json:"phone"`
	Name            *string    `json:"name,omitempty"`
	PhoneVerifiedAt *time.Time `json:"phone_verified_at,omitempty"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type OTPVerification struct {
	ID         uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey"`
	Phone      string     `json:"phone"`
	OTPHash    string     `json:"-"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Attempts   int        `json:"-"`
	Verified   bool       `json:"-"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	ConsumedAt *time.Time `json:"consumed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type UserSession struct {
	ID               uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey"`
	UserID           uuid.UUID  `json:"user_id" gorm:"type:uuid;index"`
	RefreshTokenHash string     `json:"-"`
	ExpiresAt        time.Time  `json:"expires_at"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type UserAddress struct {
	ID                   uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	UserID               uuid.UUID `json:"-" gorm:"type:uuid;index;not null"`
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
