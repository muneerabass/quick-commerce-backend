package handlers

import (
	"sync"
	"time"

	"quick-commerce/internal/api/middleware"

	"github.com/gofiber/fiber/v2"
)

// userLimiter is a small fixed-window limiter keyed by signed-in user (IP when
// anonymous), so a stuck client or script can't hammer cart and order endpoints.
// State is per process, which is enough to blunt abuse; it is not a quota.
func userLimiter(max int, window time.Duration) fiber.Handler {
	type bucket struct {
		count   int
		resetAt time.Time
	}
	var (
		mu      sync.Mutex
		buckets = map[string]*bucket{}
	)
	return func(c *fiber.Ctx) error {
		key := c.IP()
		if id, ok := middleware.UserID(c); ok {
			key = id.String()
		}
		now := time.Now()
		mu.Lock()
		b := buckets[key]
		if b == nil || now.After(b.resetAt) {
			b = &bucket{resetAt: now.Add(window)}
			buckets[key] = b
			// Opportunistic cleanup keeps the map from growing without bound.
			if len(buckets) > 10000 {
				for k, v := range buckets {
					if now.After(v.resetAt) {
						delete(buckets, k)
					}
				}
			}
		}
		b.count++
		over := b.count > max
		mu.Unlock()
		if over {
			c.Set("Retry-After", "30")
			return writeErrorCode(c, fiber.StatusTooManyRequests, "rate_limited", "Too many requests. Please slow down and try again.")
		}
		return c.Next()
	}
}
