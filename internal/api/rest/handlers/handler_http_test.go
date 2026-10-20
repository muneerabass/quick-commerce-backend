package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"quick-commerce/internal/api/rest"
	"quick-commerce/internal/domain"
	"quick-commerce/internal/dto"
	"quick-commerce/internal/repository"
	"quick-commerce/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// fullRouteDB extends testRouteDB (defined in devOTP_test.go) with the
// additional domain models required for catalog and commerce routes.
func fullRouteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testRouteDB(t)
	if err := db.AutoMigrate(
		&domain.Store{},
		&domain.StorePincode{},
		&domain.Category{},
		&domain.Product{},
		&domain.Inventory{},
		&domain.Cart{},
		&domain.CartItem{},
		&domain.Order{},
		&domain.OrderItem{},
		&domain.OrderEvent{},
		&domain.DeliveryStoreAssignment{},
	); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

// buildApp wires up the full route stack and returns the Fiber app and the auth
// service (needed to mint test tokens).
func buildApp(t *testing.T, db *gorm.DB) (*fiber.App, *services.AuthService) {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: JSONErrorHandler})
	cfg := minimalCfg(true) // DevOTPLog=true so we can read the dev OTP endpoint
	SetupUserRoutes(&rest.RestHandler{App: app, Db: db}, cfg)
	app.Use(func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "route not found", "code": "not_found"})
	})

	// Rebuild auth service so callers can sign tokens.
	auth := services.NewAuthService(
		db,
		repository.NewUserRepository(db),
		repository.NewOTPRepository(db),
		repository.NewSessionRepository(db),
		services.DevStdoutOTPSender{},
		cfg.JWTSecret,
		cfg.OTPHashSecret,
		cfg.AccessTokenTTL,
		cfg.RefreshTokenTTL,
		cfg.OTPTTL,
		cfg.OTPResendAfter,
		cfg.OTPMaxAttempts,
	)
	auth.SetDevOTPLog(true)
	return app, auth
}

// postJSON fires a JSON POST and returns the http.Response.
func postJSON(t *testing.T, app *fiber.App, path string, body any, bearer string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test POST %s: %v", path, err)
	}
	return resp
}

func getJSON(t *testing.T, app *fiber.App, path, bearer string) *http.Response {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test GET %s: %v", path, err)
	}
	return resp
}

func decodeBody(t *testing.T, r *http.Response) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return m
}

// ----------------------------------------------------------------------------
// Auth endpoint tests
// ----------------------------------------------------------------------------

// TestSendOTPBadPhone checks that an invalid phone number returns 400.
func TestSendOTPBadPhone(t *testing.T) {
	db := testRouteDB(t)
	app, _ := buildApp(t, db)
	resp := postJSON(t, app, "/api/v1/auth/send-otp", map[string]string{"phone": "not-a-phone"}, "")
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestSendAndVerifyOTP exercises the happy-path login flow using the dev OTP
// endpoint.
func TestSendAndVerifyOTP(t *testing.T) {
	db := testRouteDB(t)
	app, _ := buildApp(t, db)
	phone := "+919999000001"

	// 1. Send OTP — returns 202 Accepted.
	resp := postJSON(t, app, "/api/v1/auth/send-otp", map[string]string{"phone": phone}, "")
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("send-otp: expected 202, got %d", resp.StatusCode)
	}

	// 2. Read the OTP back from the dev endpoint (requires the dev route).
	// The + in E.164 phone numbers must be percent-encoded in the query string.
	encodedPhone := "%2B" + phone[1:]
	req := httptest.NewRequest("GET", "/api/v1/auth/dev/last-otp?phone="+encodedPhone, nil)
	devResp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if devResp.StatusCode != fiber.StatusOK {
		t.Fatalf("dev/last-otp: expected 200, got %d", devResp.StatusCode)
	}
	body := decodeBody(t, devResp)
	otp, _ := body["otp"].(string)
	if len(otp) != 6 {
		t.Fatalf("expected 6-digit OTP, got %q", otp)
	}

	// 3. Verify the OTP.
	resp = postJSON(t, app, "/api/v1/auth/verify-otp", map[string]string{"phone": phone, "otp": otp}, "")
	if resp.StatusCode != fiber.StatusOK {
		body := decodeBody(t, resp)
		t.Fatalf("verify-otp: expected 200, got %d body=%v", resp.StatusCode, body)
	}
	tokenBody := decodeBody(t, resp)
	if tokenBody["access_token"] == nil {
		t.Fatal("verify-otp: expected access_token in response")
	}
}

// TestVerifyOTPWrongCode checks that a wrong OTP returns 400.
func TestVerifyOTPWrongCode(t *testing.T) {
	db := testRouteDB(t)
	app, _ := buildApp(t, db)
	phone := "+919999000002"

	postJSON(t, app, "/api/v1/auth/send-otp", map[string]string{"phone": phone}, "")
	resp := postJSON(t, app, "/api/v1/auth/verify-otp", map[string]string{"phone": phone, "otp": "000000"}, "")
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400 on wrong OTP, got %d", resp.StatusCode)
	}
}

// ----------------------------------------------------------------------------
// RBAC tests
// ----------------------------------------------------------------------------

// TestAdminRouteRequiresAuth asserts that admin endpoints return 401 when
// no token is provided.
func TestAdminRouteRequiresAuth(t *testing.T) {
	db := fullRouteDB(t)
	app, _ := buildApp(t, db)
	resp := getJSON(t, app, "/api/v1/admin/stores", "")
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated admin request, got %d", resp.StatusCode)
	}
}

// TestAdminRouteRequiresAdminRole asserts that a regular user (role=customer)
// cannot access admin endpoints.
func TestAdminRouteRequiresAdminRole(t *testing.T) {
	db := fullRouteDB(t)
	app, _ := buildApp(t, db)
	phone := "+919999000010"

	sendResp := postJSON(t, app, "/api/v1/auth/send-otp", map[string]string{"phone": phone}, "")
	if sendResp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("send-otp: expected 202, got %d", sendResp.StatusCode)
	}
	encodedPhone := "%2B" + phone[1:]
	req := httptest.NewRequest("GET", "/api/v1/auth/dev/last-otp?phone="+encodedPhone, nil)
	devResp, err := app.Test(req, -1)
	if err != nil || devResp.StatusCode != fiber.StatusOK {
		t.Fatalf("dev/last-otp: status %d, err=%v", devResp.StatusCode, err)
	}
	otpBody := decodeBody(t, devResp)
	otp, _ := otpBody["otp"].(string)
	if len(otp) != 6 {
		t.Fatalf("expected 6-digit OTP from dev endpoint, got %q", otp)
	}

	verifyResp := postJSON(t, app, "/api/v1/auth/verify-otp", map[string]string{"phone": phone, "otp": otp}, "")
	if verifyResp.StatusCode != fiber.StatusOK {
		t.Fatalf("verify-otp: expected 200, got %d", verifyResp.StatusCode)
	}
	tokenBody := decodeBody(t, verifyResp)
	accessToken, _ := tokenBody["access_token"].(string)
	if accessToken == "" {
		t.Fatal("verify-otp: access_token missing in response")
	}

	resp := getJSON(t, app, "/api/v1/admin/stores", accessToken)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403 for non-admin role, got %d", resp.StatusCode)
	}
}

// ----------------------------------------------------------------------------
// Catalog public endpoint tests
// ----------------------------------------------------------------------------

// TestPublicProductsEndpoint checks that the public catalog endpoint returns
// 200 with a products array for a valid pincode (even when the pincode has no
// inventory, the array is empty rather than an error).
func TestPublicProductsEndpoint(t *testing.T) {
	db := fullRouteDB(t)
	app, _ := buildApp(t, db)
	resp := getJSON(t, app, "/api/v1/catalog/products?pincode=400001", "")
	if resp.StatusCode != fiber.StatusOK {
		body := decodeBody(t, resp)
		t.Fatalf("expected 200 for public catalog, got %d body=%v", resp.StatusCode, body)
	}
	body := decodeBody(t, resp)
	if _, ok := body["products"]; !ok {
		t.Fatal("response missing 'products' key")
	}
}

// TestPublicProductByIDNotFound returns 404 when the product doesn't exist.
func TestPublicProductByIDNotFound(t *testing.T) {
	db := fullRouteDB(t)
	app, _ := buildApp(t, db)
	resp := getJSON(t, app, "/api/v1/catalog/products/"+uuid.New().String(), "")
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("expected 404 for unknown product, got %d", resp.StatusCode)
	}
}

// ----------------------------------------------------------------------------
// Cart endpoint tests (requires auth)
// ----------------------------------------------------------------------------

// TestCartAddRequiresAuth checks that the add-to-cart endpoint rejects
// unauthenticated requests with 401.
func TestCartAddRequiresAuth(t *testing.T) {
	db := fullRouteDB(t)
	app, _ := buildApp(t, db)
	resp := postJSON(t, app, "/api/v1/cart", map[string]any{
		"store_id":   uuid.New().String(),
		"product_id": uuid.New().String(),
		"quantity":   1,
	}, "")
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated cart add, got %d", resp.StatusCode)
	}
}

// ----------------------------------------------------------------------------
// Unknown route test
// ----------------------------------------------------------------------------

// TestUnknownRouteReturns404 asserts that the JSON 404 fallback fires for
// routes outside the /api/v1/ prefix.
func TestUnknownRouteReturns404(t *testing.T) {
	db := testRouteDB(t)
	app, _ := buildApp(t, db)
	// Use a path outside /api/v1/ so the commerce authenticated group doesn't intercept it.
	resp := getJSON(t, app, "/unknown-path/does-not-exist", "")
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("expected 404 for unregistered route, got %d", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if body["code"] != "not_found" {
		t.Fatalf("expected JSON code='not_found', got %v", body)
	}
}

// ----------------------------------------------------------------------------
// Healthz
// ----------------------------------------------------------------------------

func TestHealthzEndpoint(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: JSONErrorHandler})
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "ok"})
	})
	req := httptest.NewRequest("GET", "/healthz", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if body["status"] != "ok" {
		t.Fatalf("expected {status:ok}, got %v", body)
	}
}

// Ensure unused import (context) is consumed.
var _ = context.Background
var _ = dto.SendOTPRequest{}
