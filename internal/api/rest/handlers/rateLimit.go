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
//
// M-008 — per-process limitation: the bucket map is held in memory inside a
// single Go process. When the application runs as more than one replica (behind
// a load balancer or as multiple containers), each replica maintains its own
// independent counter. A client can therefore send up to max × N requests per
// window across N replicas before being throttled, making this a best-effort
// defence, not a hard cap. To enforce a global quota across replicas, replace
// this implementation with a distributed counter (e.g. Redis INCR + EXPIRE or
// a dedicated rate-limit sidecar). Until then, set max conservatively and rely
// on the upstream WAF or API gateway for stricter global limits.
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
