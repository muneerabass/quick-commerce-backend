package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrGoogleMapsNotConfigured = errors.New("Google Maps is not configured")
	ErrGoogleMaps              = errors.New("Google Maps request failed")
	ErrAddressNotFound         = errors.New("address could not be geocoded")
)

type GeocodeResult struct {
	FormattedAddress string
	Latitude         float64
	Longitude        float64
}

type Geocoder interface {
	Geocode(ctx context.Context, address string) (GeocodeResult, error)
}

type GoogleMapsGeocoder struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewGoogleMapsGeocoder(baseURL, apiKey string) *GoogleMapsGeocoder {
	return &GoogleMapsGeocoder{
		baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *GoogleMapsGeocoder) Geocode(ctx context.Context, address string) (GeocodeResult, error) {
	if strings.TrimSpace(g.apiKey) == "" {
		return GeocodeResult{}, ErrGoogleMapsNotConfigured
	}
	if strings.TrimSpace(address) == "" {
		return GeocodeResult{}, fmt.Errorf("%w: address is required", ErrInvalidInput)
	}
	query := url.Values{}
	query.Set("address", address)
	query.Set("key", g.apiKey)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+"/maps/api/geocode/json?"+query.Encode(), nil)
	if err != nil {
		return GeocodeResult{}, ErrGoogleMaps
	}
	response, err := g.httpClient.Do(request)
	if err != nil {
		return GeocodeResult{}, ErrGoogleMaps
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 256<<10))
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return GeocodeResult{}, ErrGoogleMaps
	}
	var payload struct {
		Results []struct {
			FormattedAddress string `json:"formatted_address"`
			Geometry         struct {
				Location struct {
					Latitude  float64 `json:"lat"`
					Longitude float64 `json:"lng"`
				} `json:"location"`
			} `json:"geometry"`
		} `json:"results"`
		Status       string `json:"status"`
		ErrorMessage string `json:"error_message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return GeocodeResult{}, ErrGoogleMaps
	}
	if payload.Status == "ZERO_RESULTS" || len(payload.Results) == 0 {
		return GeocodeResult{}, ErrAddressNotFound
	}
	if payload.Status != "OK" {
		return GeocodeResult{}, ErrGoogleMaps
	}
	result := payload.Results[0]
	return GeocodeResult{FormattedAddress: result.FormattedAddress, Latitude: result.Geometry.Location.Latitude, Longitude: result.Geometry.Location.Longitude}, nil
}
