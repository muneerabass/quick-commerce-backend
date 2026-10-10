package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"

	"github.com/google/uuid"
)

type testGeocoder struct{}

func (testGeocoder) Geocode(context.Context, string) (GeocodeResult, error) {
	return GeocodeResult{FormattedAddress: "Resolved Address", Latitude: 12.9716, Longitude: 77.5946}, nil
}

func TestGoogleMapsGeocoder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/maps/api/geocode/json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-key" || r.URL.Query().Get("address") != "MG Road, Bengaluru" {
			t.Fatalf("unexpected query: %v", r.URL.Query())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"OK","results":[{"formatted_address":"MG Road, Bengaluru, Karnataka, India","geometry":{"location":{"lat":12.9716,"lng":77.5946}}}]}`))
	}))
	defer server.Close()

	geocoder := NewGoogleMapsGeocoder(server.URL, "test-key")
	result, err := geocoder.Geocode(context.Background(), "MG Road, Bengaluru")
	if err != nil {
		t.Fatal(err)
	}
	if result.Latitude != 12.9716 || result.Longitude != 77.5946 || !strings.Contains(result.FormattedAddress, "Bengaluru") {
		t.Fatalf("unexpected geocode result: %+v", result)
	}
}

func TestGoogleMapsGeocoderZeroResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ZERO_RESULTS","results":[]}`))
	}))
	defer server.Close()

	geocoder := NewGoogleMapsGeocoder(server.URL, "test-key")
	if _, err := geocoder.Geocode(context.Background(), "unknown"); err != ErrAddressNotFound {
		t.Fatalf("expected address not found, got %v", err)
	}
}

func TestAddressServiceResolvesMissingCoordinates(t *testing.T) {
	db := testDB(t)
	service := NewAddressServiceWithGeocoder(db, repository.NewUserRepository(db), repository.NewAddressRepository(db), testGeocoder{})
	userID := uuid.New()
	address, err := service.Create(userID, dto.AddressRequest{Label: "Home", RecipientName: "Muneer", Phone: "+919876543240", AddressLine1: "MG Road", City: "Bengaluru", State: "Karnataka", Pincode: "560001"})
	if err != nil {
		t.Fatal(err)
	}
	if address.Latitude == nil || address.Longitude == nil || *address.Latitude != 12.9716 || *address.Longitude != 77.5946 {
		t.Fatalf("expected resolved coordinates, address=%+v", address)
	}
}
