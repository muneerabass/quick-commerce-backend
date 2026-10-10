package dto

type UserResponseDto struct {
	ID              string  `json:"id"`
	Phone           string  `json:"phone"`
	Name            *string `json:"name,omitempty"`
	PhoneVerifiedAt *string `json:"phone_verified_at,omitempty"`
	Status          string  `json:"status"`
	Role            string  `json:"role"`
}

type AuthResponse struct {
	User         UserResponseDto `json:"user"`
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	TokenType    string          `json:"token_type"`
	ExpiresIn    int64           `json:"expires_in"`
	SessionID    string          `json:"session_id"`
}
