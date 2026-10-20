package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"quick-commerce/internal/dto"
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
		{fmt.Errorf("%w: a store named %q already exists", services.ErrDuplicateName, "Fresh Mart"), 409, "duplicate_name"},
		{fmt.Errorf("%w: pincode is required", services.ErrInvalidInput), 400, "invalid_input"},
		{repository.ErrNotFound, 404, "not_found"},
		{services.ErrNotServiceable, 422, "not_serviceable"},
		{services.ErrCartStoreMismatch, 409, "cart_store_mismatch"},
		{services.ErrInsufficientStock, 409, "insufficient_stock"},
		{services.ErrEmptyCart, 400, "empty_cart"},
		{services.ErrOrderNotCancellable, 409, "order_not_cancellable"},
		{&services.InvalidDeliveryCodeError{Remaining: 3}, 422, "invalid_delivery_code"},
		{services.ErrDeliveryCodeLocked, 409, "delivery_code_locked"},
		{services.ErrActiveOrders, 409, "active_orders"},
		{&services.CartUnavailableError{Issues: []dto.CartIssue{{Code: "out_of_stock", Message: "Milk is out of stock"}}}, 409, "cart_unavailable"},
		{errors.New("boom"), 500, "internal"},
	}
	for _, tc := range cases {
		app := fiber.New()
		app.Get("/", func(c *fiber.Ctx) error { return serviceError(c, tc.err) })
		resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != tc.status || body["code"] != tc.code || body["error"] == "" {
			t.Errorf("%v: got %d %v, want %d %s", tc.err, resp.StatusCode, body, tc.status, tc.code)
		}
	}
}
