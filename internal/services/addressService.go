package services

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"quick-commerce/internal/domain"
	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const maxAddressesPerUser = 20

var ErrAddressLimitReached = errors.New("address limit reached: you may store at most 20 addresses")

var pincodePattern = regexp.MustCompile(`^[0-9]{4,10}$`)

type AddressService struct {
	db       *gorm.DB
	users    repository.UserRepository
	addrs    repository.AddressRepository
	geocoder Geocoder
}

func NewAddressService(db *gorm.DB, users repository.UserRepository, addrs repository.AddressRepository) *AddressService {
	return &AddressService{db: db, users: users, addrs: addrs}
}

func NewAddressServiceWithGeocoder(db *gorm.DB, users repository.UserRepository, addrs repository.AddressRepository, geocoder Geocoder) *AddressService {
	return &AddressService{db: db, users: users, addrs: addrs, geocoder: geocoder}
}

func (s *AddressService) Geocode(address string) (dto.GeocodeResponse, error) {
	address = strings.TrimSpace(address)
	if address == "" || len(address) > 500 {
		return dto.GeocodeResponse{}, fmt.Errorf("%w: address must be between 1 and 500 characters", ErrInvalidInput)
	}
	if s.geocoder == nil {
		return dto.GeocodeResponse{}, ErrGoogleMapsNotConfigured
	}
	result, err := s.geocoder.Geocode(context.Background(), address)
	if err != nil {
		return dto.GeocodeResponse{}, err
	}
	return dto.GeocodeResponse{FormattedAddress: result.FormattedAddress, Latitude: result.Latitude, Longitude: result.Longitude}, nil
}

func (s *AddressService) List(userID uuid.UUID) ([]dto.AddressResponse, error) {
	addresses, err := s.addrs.List(nil, userID)
	if err != nil {
		return nil, err
	}
	return mapAddresses(addresses), nil
}

func (s *AddressService) Get(userID, addressID uuid.UUID) (dto.AddressResponse, error) {
	address, err := s.addrs.Find(nil, userID, addressID)
	if err != nil {
		return dto.AddressResponse{}, err
	}
	return mapAddress(address), nil
}

func (s *AddressService) Create(userID uuid.UUID, request dto.AddressRequest) (dto.AddressResponse, error) {
	address, err := addressFromRequest(userID, request)
	if err != nil {
		return dto.AddressResponse{}, err
	}
	if err := s.resolveCoordinates(address); err != nil {
		return dto.AddressResponse{}, err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressChanges(tx, userID); err != nil {
			return err
		}
		count, err := s.addrs.Count(tx, userID)
		if err != nil {
			return err
		}
		// M-006: enforce per-user address cap inside the lock so concurrent
		// creates cannot race past the limit.
		if count >= maxAddressesPerUser {
			return ErrAddressLimitReached
		}
		makeDefault := count == 0 || (request.IsDefault != nil && *request.IsDefault)
		if makeDefault {
			if err := s.addrs.ClearDefaults(tx, userID); err != nil {
				return err
			}
		}
		address.IsDefault = makeDefault
		return s.addrs.Create(tx, address)
	})
	if err != nil {
		return dto.AddressResponse{}, err
	}
	return mapAddress(address), nil
}

func (s *AddressService) Update(userID, addressID uuid.UUID, request dto.AddressPatchRequest) (dto.AddressResponse, error) {
	var updated *domain.UserAddress
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressChanges(tx, userID); err != nil {
			return err
		}
		address, err := s.addrs.Find(tx, userID, addressID)
		if err != nil {
			return err
		}
		if err := applyAddressPatch(address, request); err != nil {
			return err
		}
		if err := s.resolveCoordinates(address); err != nil {
			return err
		}
		if request.IsDefault != nil && *request.IsDefault {
			if err := s.addrs.ClearDefaults(tx, userID); err != nil {
				return err
			}
			address.IsDefault = true
		}
		// is_default=false on the current default is a no-op, not an error: the
		// default only moves when another address is made the default. This lets
		// the default address be edited like any other.
		if err := s.addrs.Update(tx, userID, address); err != nil {
			return err
		}
		updated = address
		return nil
	})
	if err != nil {
		return dto.AddressResponse{}, err
	}
	return mapAddress(updated), nil
}

func (s *AddressService) resolveCoordinates(address *domain.UserAddress) error {
	if address.Latitude != nil && address.Longitude != nil {
		return nil
	}
	if s.geocoder == nil {
		return nil
	}
	query := strings.Join([]string{address.AddressLine1, address.City, address.State, address.Pincode}, ", ")
	result, err := s.geocoder.Geocode(context.Background(), query)
	if err != nil {
		return err
	}
	address.Latitude = &result.Latitude
	address.Longitude = &result.Longitude
	return nil
}

func (s *AddressService) Delete(userID, addressID uuid.UUID) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressChanges(tx, userID); err != nil {
			return err
		}
		address, err := s.addrs.Find(tx, userID, addressID)
		if err != nil {
			return err
		}
		if err := s.addrs.Delete(tx, userID, addressID); err != nil {
			return err
		}
		if address.IsDefault {
			var replacement []domain.UserAddress
			all, err := s.addrs.List(tx, userID)
			if err != nil {
				return err
			}
			replacement = all
			if len(replacement) > 0 {
				if err := s.addrs.ClearDefaults(tx, userID); err != nil {
					return err
				}
				replacement[0].IsDefault = true
				if err := s.addrs.Update(tx, userID, &replacement[0]); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *AddressService) SetDefault(userID, addressID uuid.UUID) (dto.AddressResponse, error) {
	var selected *domain.UserAddress
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressChanges(tx, userID); err != nil {
			return err
		}
		var err error
		selected, err = s.addrs.Find(tx, userID, addressID)
		if err != nil {
			return err
		}
		if err := s.addrs.ClearDefaults(tx, userID); err != nil {
			return err
		}
		selected.IsDefault = true
		return s.addrs.Update(tx, userID, selected)
	})
	if err != nil {
		return dto.AddressResponse{}, err
	}
	return mapAddress(selected), nil
}

func addressFromRequest(userID uuid.UUID, request dto.AddressRequest) (*domain.UserAddress, error) {
	phone, err := NormalizePhone(request.Phone)
	if err != nil {
		return nil, err
	}
	address := &domain.UserAddress{
		UserID: userID, Label: strings.TrimSpace(request.Label), RecipientName: strings.TrimSpace(request.RecipientName),
		Phone: phone, AddressLine1: strings.TrimSpace(request.AddressLine1), AddressLine2: trimOptional(request.AddressLine2),
		Landmark: trimOptional(request.Landmark), City: strings.TrimSpace(request.City), State: strings.TrimSpace(request.State),
		Pincode: strings.TrimSpace(request.Pincode), Latitude: request.Latitude, Longitude: request.Longitude,
		DeliveryInstructions: trimOptional(request.DeliveryInstructions), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := validateAddress(address); err != nil {
		return nil, err
	}
	return address, nil
}

func applyAddressPatch(address *domain.UserAddress, request dto.AddressPatchRequest) error {
	locationChanged := false
	if request.Label != nil {
		address.Label = strings.TrimSpace(*request.Label)
	}
	if request.RecipientName != nil {
		address.RecipientName = strings.TrimSpace(*request.RecipientName)
	}
	if request.Phone != nil {
		phone, err := NormalizePhone(*request.Phone)
		if err != nil {
			return err
		}
		address.Phone = phone
	}
	if request.AddressLine1 != nil {
		address.AddressLine1 = strings.TrimSpace(*request.AddressLine1)
		locationChanged = true
	}
	if request.AddressLine2 != nil {
		address.AddressLine2 = trimOptional(request.AddressLine2)
	}
	if request.Landmark != nil {
		address.Landmark = trimOptional(request.Landmark)
	}
	if request.City != nil {
		address.City = strings.TrimSpace(*request.City)
		locationChanged = true
	}
	if request.State != nil {
		address.State = strings.TrimSpace(*request.State)
		locationChanged = true
	}
	if request.Pincode != nil {
		address.Pincode = strings.TrimSpace(*request.Pincode)
		locationChanged = true
	}
	if request.Latitude != nil {
		address.Latitude = request.Latitude
	}
	if request.Longitude != nil {
		address.Longitude = request.Longitude
	}
	if locationChanged && request.Latitude == nil && request.Longitude == nil {
		address.Latitude = nil
		address.Longitude = nil
	}
	if request.DeliveryInstructions != nil {
		address.DeliveryInstructions = trimOptional(request.DeliveryInstructions)
	}
	address.UpdatedAt = time.Now().UTC()
	return validateAddress(address)
}

func validateAddress(address *domain.UserAddress) error {
	if address.Label != "Home" && address.Label != "Work" && address.Label != "Other" {
		return fmt.Errorf("%w: label must be Home, Work, or Other", ErrInvalidInput)
	}
	fields := map[string]string{"recipient_name": address.RecipientName, "address_line1": address.AddressLine1, "city": address.City, "state": address.State, "pincode": address.Pincode}
	for field, value := range fields {
		if value == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidInput, field)
		}
		if len(value) > 200 {
			return fmt.Errorf("%w: %s is too long", ErrInvalidInput, field)
		}
	}
	if !pincodePattern.MatchString(address.Pincode) {
		return fmt.Errorf("%w: pincode must contain 4 to 10 digits", ErrInvalidInput)
	}
	if address.Latitude != nil && (*address.Latitude < -90 || *address.Latitude > 90) {
		return fmt.Errorf("%w: latitude must be between -90 and 90", ErrInvalidInput)
	}
	if address.Longitude != nil && (*address.Longitude < -180 || *address.Longitude > 180) {
		return fmt.Errorf("%w: longitude must be between -180 and 180", ErrInvalidInput)
	}
	return nil
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func mapAddresses(addresses []domain.UserAddress) []dto.AddressResponse {
	responses := make([]dto.AddressResponse, 0, len(addresses))
	for i := range addresses {
		responses = append(responses, mapAddress(&addresses[i]))
	}
	return responses
}

func mapAddress(address *domain.UserAddress) dto.AddressResponse {
	return dto.AddressResponse{ID: address.ID.String(), Label: address.Label, RecipientName: address.RecipientName, Phone: address.Phone, AddressLine1: address.AddressLine1, AddressLine2: address.AddressLine2, Landmark: address.Landmark, City: address.City, State: address.State, Pincode: address.Pincode, Latitude: address.Latitude, Longitude: address.Longitude, DeliveryInstructions: address.DeliveryInstructions, IsDefault: address.IsDefault, CreatedAt: address.CreatedAt, UpdatedAt: address.UpdatedAt}
}

func lockAddressChanges(tx *gorm.DB, userID uuid.UUID) error {
	if tx.Dialector.Name() != "postgres" {
		return nil
	}
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", userID.String()).Error
}
