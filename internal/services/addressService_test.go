package services

import (
	"errors"
	"testing"

	"quick-commerce/internal/domain"
	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"

	"github.com/google/uuid"
)

func TestAddressServiceOwnershipAndDefaults(t *testing.T) {
	db := testDB(t)
	users := repository.NewUserRepository(db)
	addresses := NewAddressService(db, users, repository.NewAddressRepository(db))
	firstUser := uuid.New()
	secondUser := uuid.New()
	for _, id := range []uuid.UUID{firstUser, secondUser} {
		name := "Test User"
		if err := db.Create(&domain.User{ID: id, Phone: "+919876543220", Name: &name, Status: "active"}).Error; err != nil {
			t.Fatal(err)
		}
	}

	request := dto.AddressRequest{Label: "Home", RecipientName: "Recipient", Phone: "+919876543220", AddressLine1: "1 Main Street", City: "Mumbai", State: "MH", Pincode: "400001"}
	first, err := addresses.Create(firstUser, request)
	if err != nil {
		t.Fatal(err)
	}
	if !first.IsDefault {
		t.Fatal("first address should be default")
	}
	request.Label = "Work"
	second, err := addresses.Create(firstUser, request)
	if err != nil {
		t.Fatal(err)
	}
	if second.IsDefault {
		t.Fatal("second address should not be default")
	}
	if _, err := addresses.Get(secondUser, uuid.MustParse(first.ID)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ownership check, got %v", err)
	}
	updated, err := addresses.SetDefault(firstUser, uuid.MustParse(second.ID))
	if err != nil || !updated.IsDefault {
		t.Fatalf("expected default update, address=%+v err=%v", updated, err)
	}
	if err := addresses.Delete(firstUser, uuid.MustParse(second.ID)); err != nil {
		t.Fatal(err)
	}
	line2 := "Apartment 2"
	patched, err := addresses.Update(firstUser, uuid.MustParse(first.ID), dto.AddressPatchRequest{AddressLine2: &line2})
	if err != nil || patched.AddressLine2 == nil || *patched.AddressLine2 != line2 {
		t.Fatalf("expected address patch, address=%+v err=%v", patched, err)
	}
	remaining, err := addresses.Get(firstUser, uuid.MustParse(first.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !remaining.IsDefault {
		t.Fatal("deleting default should promote remaining address")
	}
}
