package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"quick-commerce/internal/repository"
	"quick-commerce/internal/services"

	"github.com/gofiber/fiber/v2"
)

func TestServiceErrorStatusAndCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("%w: pincode is required", services.ErrInvalidInput), 400, "invalid_input"},
		{repository.ErrNotFound, 404, "not_found"},
		{services.ErrNotServiceable, 422, "not_serviceable"},
		{services.ErrCartStoreMismatch, 409, "cart_store_mismatch"},
		{services.ErrInsufficientStock, 409, "insufficient_stock"},
		{services.ErrEmptyCart, 400, "empty_cart"},
		{errors.New("boom"), 500, "internal"},
	}
	for _, tc := range cases {
		app := fiber.New()
		app.Get("/", func(c *fiber.Ctx) error { return serviceError(c, tc.err) })
		resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != tc.status || body["code"] != tc.code || body["error"] == "" {
			t.Errorf("%v: got %d %v, want %d %s", tc.err, resp.StatusCode, body, tc.status, tc.code)
		}
	}
}
