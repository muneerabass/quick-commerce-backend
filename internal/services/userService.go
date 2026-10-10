package services

import (
	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"

	"github.com/google/uuid"
)

// UserService contains user lookups that are independent of authentication.
// Authentication and profile mutations live in AuthService because they share
// the OTP and session transaction boundaries.
type UserService struct {
	users repository.UserRepository
}

func NewUserService(users repository.UserRepository) *UserService {
	return &UserService{users: users}
}

func (s *UserService) GetProfile(userID uuid.UUID) (dto.UserResponseDto, error) {
	user, err := s.users.FindByID(nil, userID)
	if err != nil {
		return dto.UserResponseDto{}, err
	}
	return userResponse(user), nil
}
