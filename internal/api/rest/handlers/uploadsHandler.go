package handlers

// Cloudinary signed uploads: the client never sees the API secret. We hand it a
// short-lived signature (SHA-1 of alphabetically-sorted "key=value" params + the
// secret, hex-encoded) and the client uploads directly to Cloudinary with it.

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"quick-commerce/configs"
	"quick-commerce/internal/api/middleware"
	"quick-commerce/internal/api/rest"
	"quick-commerce/internal/services"

	"github.com/gofiber/fiber/v2"
)

type UploadsHandler struct {
	cloudName string
	apiKey    string
	apiSecret string
}

// SetupUploadsRoutes mounts the /admin/uploads routes when Cloudinary is
// configured. Admin-role required.
func SetupUploadsRoutes(rh *rest.RestHandler, cfg configs.AppConfig, auth *services.AuthService) {
	if cfg.CloudinaryCloudName == "" || cfg.CloudinaryAPIKey == "" || cfg.CloudinaryAPISecret == "" {
		return
	}
	h := &UploadsHandler{cloudName: cfg.CloudinaryCloudName, apiKey: cfg.CloudinaryAPIKey, apiSecret: cfg.CloudinaryAPISecret}
	api := rh.App.Group("/api/v1")
	authenticated := middleware.RequireAuth(auth.TokenService(), auth.ValidateAccessSession)
	admin := api.Group("/admin", authenticated, middleware.RequireRole(auth.UserRole, "admin"))
	admin.Post("/uploads/signature", h.Signature)
}

type signatureRequest struct {
	Folder string `json:"folder"`
}

type signatureResponse struct {
	CloudName string `json:"cloud_name"`
	APIKey    string `json:"api_key"`
	Timestamp int64  `json:"timestamp"`
	Signature string `json:"signature"`
	Folder    string `json:"folder,omitempty"`
}

// Signature returns credentials for one direct-upload call to Cloudinary. The
// returned signature stays valid for ~1 hour from the timestamp; the client is
// expected to use it immediately.
func (h *UploadsHandler) Signature(c *fiber.Ctx) error {
	var request signatureRequest
	// Body is optional — a missing / empty body is fine.
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&request); err != nil {
			return writeError(c, fiber.StatusBadRequest, "invalid request body")
		}
	}
	folder := strings.TrimSpace(request.Folder)
	if folder == "" {
		folder = "quick-commerce/products"
	}
	timestamp := time.Now().Unix()

	// Params that will be included in the upload AND must be signed. Keep this
	// in strict alphabetical order per Cloudinary's spec.
	params := map[string]string{
		"folder":    folder,
		"timestamp": fmt.Sprintf("%d", timestamp),
	}
	signature := cloudinarySign(params, h.apiSecret)

	return c.JSON(signatureResponse{
		CloudName: h.cloudName,
		APIKey:    h.apiKey,
		Timestamp: timestamp,
		Signature: signature,
		Folder:    folder,
	})
}

// cloudinarySign builds the signature string Cloudinary expects: keys sorted
// alphabetically, joined "key=value" with "&", then "+ api_secret" appended,
// SHA-1 hex-encoded.
func cloudinarySign(params map[string]string, apiSecret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, params[k]))
	}
	toSign := strings.Join(pairs, "&") + apiSecret
	sum := sha1.Sum([]byte(toSign))
	return hex.EncodeToString(sum[:])
}
