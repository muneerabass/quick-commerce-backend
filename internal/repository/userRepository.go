package repository

import (
	"errors"
	"quick-commerce/internal/domain"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrNotFound = gorm.ErrRecordNotFound

type UserRepository interface {
	FindByID(db *gorm.DB, id uuid.UUID) (*domain.User, error)
	FindByPhone(db *gorm.DB, phone string) (*domain.User, error)
	Create(db *gorm.DB, user *domain.User) error
	Update(db *gorm.DB, user *domain.User) error
	Anonymize(db *gorm.DB, id uuid.UUID) error
	ListDeliveryForStore(db *gorm.DB, storeID uuid.UUID) ([]*domain.User, error)
}

type userRepository struct{ db *gorm.DB }

func NewUserRepository(db *gorm.DB) UserRepository { return &userRepository{db: db} }

func (r *userRepository) FindByID(db *gorm.DB, id uuid.UUID) (*domain.User, error) {
	var user domain.User
	err := dbOrDefault(db, r.db).Where("id = ? AND status <> ?", id, "deleted").First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &user, err
}

func (r *userRepository) FindByPhone(db *gorm.DB, phone string) (*domain.User, error) {
	var user domain.User
	err := dbOrDefault(db, r.db).Where("phone = ? AND status <> ?", phone, "deleted").First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &user, err
}

func (r *userRepository) Create(db *gorm.DB, user *domain.User) error {
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	return dbOrDefault(db, r.db).Create(user).Error
}

func (r *userRepository) Update(db *gorm.DB, user *domain.User) error {
	return dbOrDefault(db, r.db).Save(user).Error
}

func (r *userRepository) Anonymize(db *gorm.DB, id uuid.UUID) error {
	query := dbOrDefault(db, r.db).Model(&domain.User{}).
		Where("id = ? AND status <> ?", id, "deleted").
		Updates(map[string]any{
			"phone":             nil,
			"name":              nil,
			"phone_verified_at": nil,
			"status":            "deleted",
			// H-003: reset role so a deleted admin no longer occupies the
			// users_one_admin_idx partial unique index (belt-and-suspenders;
			// the index itself is also fixed to exclude deleted rows).
			"role": "customer",
		})
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected == 0 {
		return ErrNotFound
	}
	if dbOrDefault(db, r.db).Dialector.Name() != "postgres" {
		return nil
	}
	// These columns belong to the legacy schema. Clear them when present, but
	// keep account deletion compatible with a fresh phone-only installation.
	for _, column := range []string{"email", "password", "code", "user_type"} {
		var exists bool
		if err := dbOrDefault(db, r.db).Raw(
			"SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'users' AND column_name = ?)", column,
		).Scan(&exists).Error; err != nil {
			return err
		}
		if exists {
			if err := dbOrDefault(db, r.db).Model(&domain.User{}).Where("id = ?", id).Update(column, nil).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *userRepository) ListDeliveryForStore(db *gorm.DB, storeID uuid.UUID) ([]*domain.User, error) {
	var users []*domain.User
	err := dbOrDefault(db, r.db).
		Joins("JOIN delivery_store_assignments ON delivery_store_assignments.delivery_id = users.id").
		Where("delivery_store_assignments.store_id = ? AND users.role = ? AND users.status <> ?", storeID, "delivery", "deleted").
		Find(&users).Error
	return users, err
}

type OTPRepository interface {
	CountRecent(db *gorm.DB, phone string, since time.Time) (int64, error)
	LatestForUpdate(db *gorm.DB, phone string) (*domain.OTPVerification, error)
	Create(db *gorm.DB, verification *domain.OTPVerification) error
	IncrementAttempts(db *gorm.DB, id uuid.UUID) error
	Consume(db *gorm.DB, id uuid.UUID, now time.Time) error
	// CountDailyByPhone counts OTPs sent to a phone since midnight of the given day
	// (UTC). Used to enforce the daily per-phone cap (M-005).
	CountDailyByPhone(db *gorm.DB, phone string, dayStart time.Time) (int64, error)
}

type otpRepository struct{ db *gorm.DB }

func NewOTPRepository(db *gorm.DB) OTPRepository { return &otpRepository{db: db} }

func (r *otpRepository) CountRecent(db *gorm.DB, phone string, since time.Time) (int64, error) {
	var count int64
	err := dbOrDefault(db, r.db).Model(&domain.OTPVerification{}).
		Where("phone = ? AND created_at >= ?", phone, since).Count(&count).Error
	return count, err
}

func (r *otpRepository) CountDailyByPhone(db *gorm.DB, phone string, dayStart time.Time) (int64, error) {
	var count int64
	err := dbOrDefault(db, r.db).Model(&domain.OTPVerification{}).
		Where("phone = ? AND created_at >= ?", phone, dayStart).Count(&count).Error
	return count, err
}

func (r *otpRepository) LatestForUpdate(db *gorm.DB, phone string) (*domain.OTPVerification, error) {
	var verification domain.OTPVerification
	query := dbOrDefault(db, r.db).Where(
		"phone = ? AND consumed_at IS NULL AND verified_at IS NULL",
		phone,
	).Order("created_at DESC").Limit(1)
	if db != nil {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.First(&verification).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &verification, err
}

func (r *otpRepository) Create(db *gorm.DB, verification *domain.OTPVerification) error {
	if verification.ID == uuid.Nil {
		verification.ID = uuid.New()
	}
	return dbOrDefault(db, r.db).Create(verification).Error
}

func (r *otpRepository) IncrementAttempts(db *gorm.DB, id uuid.UUID) error {
	return dbOrDefault(db, r.db).Model(&domain.OTPVerification{}).
		Where("id = ?", id).UpdateColumn("attempts", gorm.Expr("attempts + 1")).Error
}

func (r *otpRepository) Consume(db *gorm.DB, id uuid.UUID, now time.Time) error {
	return dbOrDefault(db, r.db).Model(&domain.OTPVerification{}).
		Where("id = ? AND consumed_at IS NULL", id).
		Updates(map[string]any{"verified": true, "verified_at": now, "consumed_at": now}).Error
}

type SessionRepository interface {
	Create(db *gorm.DB, session *domain.UserSession) error
	FindByRefreshHashForUpdate(db *gorm.DB, hash string) (*domain.UserSession, error)
	FindActive(db *gorm.DB, sessionID, userID uuid.UUID, now time.Time) error
	Revoke(db *gorm.DB, id uuid.UUID, now time.Time) error
	RevokeAll(db *gorm.DB, userID uuid.UUID, now time.Time) error
}

type sessionRepository struct{ db *gorm.DB }

func NewSessionRepository(db *gorm.DB) SessionRepository { return &sessionRepository{db: db} }

func (r *sessionRepository) Create(db *gorm.DB, session *domain.UserSession) error {
	if session.ID == uuid.Nil {
		session.ID = uuid.New()
	}
	return dbOrDefault(db, r.db).Create(session).Error
}

func (r *sessionRepository) FindByRefreshHashForUpdate(db *gorm.DB, hash string) (*domain.UserSession, error) {
	var session domain.UserSession
	query := dbOrDefault(db, r.db).Where("refresh_token_hash = ?", hash)
	if db != nil {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &session, err
}

func (r *sessionRepository) FindActive(db *gorm.DB, sessionID, userID uuid.UUID, now time.Time) error {
	var session domain.UserSession
	err := dbOrDefault(db, r.db).Where(
		"id = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ?",
		sessionID, userID, now,
	).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("session is inactive")
	}
	return err
}

func (r *sessionRepository) Revoke(db *gorm.DB, id uuid.UUID, now time.Time) error {
	return dbOrDefault(db, r.db).Model(&domain.UserSession{}).
		Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", now).Error
}

func (r *sessionRepository) RevokeAll(db *gorm.DB, userID uuid.UUID, now time.Time) error {
	return dbOrDefault(db, r.db).Model(&domain.UserSession{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).Update("revoked_at", now).Error
}

type AddressRepository interface {
	List(db *gorm.DB, userID uuid.UUID) ([]domain.UserAddress, error)
	Find(db *gorm.DB, userID, id uuid.UUID) (*domain.UserAddress, error)
	Count(db *gorm.DB, userID uuid.UUID) (int64, error)
	ClearDefaults(db *gorm.DB, userID uuid.UUID) error
	Create(db *gorm.DB, address *domain.UserAddress) error
	Update(db *gorm.DB, userID uuid.UUID, address *domain.UserAddress) error
	Delete(db *gorm.DB, userID, id uuid.UUID) error
}

type addressRepository struct{ db *gorm.DB }

func NewAddressRepository(db *gorm.DB) AddressRepository { return &addressRepository{db: db} }

func (r *addressRepository) List(db *gorm.DB, userID uuid.UUID) ([]domain.UserAddress, error) {
	var addresses []domain.UserAddress
	err := dbOrDefault(db, r.db).Where("user_id = ?", userID).Order("is_default DESC, created_at DESC").Find(&addresses).Error
	return addresses, err
}

func (r *addressRepository) Find(db *gorm.DB, userID, id uuid.UUID) (*domain.UserAddress, error) {
	var address domain.UserAddress
	err := dbOrDefault(db, r.db).Where("user_id = ? AND id = ?", userID, id).First(&address).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &address, err
}

func (r *addressRepository) Count(db *gorm.DB, userID uuid.UUID) (int64, error) {
	var count int64
	err := dbOrDefault(db, r.db).Model(&domain.UserAddress{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

func (r *addressRepository) ClearDefaults(db *gorm.DB, userID uuid.UUID) error {
	return dbOrDefault(db, r.db).Model(&domain.UserAddress{}).Where("user_id = ?", userID).Update("is_default", false).Error
}

func (r *addressRepository) Create(db *gorm.DB, address *domain.UserAddress) error {
	if address.ID == uuid.Nil {
		address.ID = uuid.New()
	}
	return dbOrDefault(db, r.db).Create(address).Error
}

func (r *addressRepository) Update(db *gorm.DB, userID uuid.UUID, address *domain.UserAddress) error {
	return dbOrDefault(db, r.db).Model(&domain.UserAddress{}).
		Where("user_id = ? AND id = ?", userID, address.ID).
		Updates(map[string]any{
			"label": address.Label, "recipient_name": address.RecipientName, "phone": address.Phone,
			"address_line1": address.AddressLine1, "address_line2": address.AddressLine2, "landmark": address.Landmark,
			"city": address.City, "state": address.State, "pincode": address.Pincode, "latitude": address.Latitude,
			"longitude": address.Longitude, "delivery_instructions": address.DeliveryInstructions,
			"is_default": address.IsDefault, "updated_at": address.UpdatedAt,
		}).Error
}

func (r *addressRepository) Delete(db *gorm.DB, userID, id uuid.UUID) error {
	return dbOrDefault(db, r.db).Where("user_id = ? AND id = ?", userID, id).Delete(&domain.UserAddress{}).Error
}

func dbOrDefault(db, fallback *gorm.DB) *gorm.DB {
	if db != nil {
		return db
	}
	return fallback
}
