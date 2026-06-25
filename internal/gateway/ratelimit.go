package gateway

import (
	"sync"
	"time"
)

// RateLimiter is a simple per-key token-bucket limiter (keyed by client IP at
// the edge). It bounds abuse without a external dependency; a production edge
// would push this to the ingress/API-gateway layer or a shared Redis bucket.
type RateLimiter struct {
	rate    float64 // tokens per second
	burst   float64 // bucket capacity
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter allows `rate` requests/sec per key with a `burst` allowance.
func NewRateLimiter(rate, burst float64) *RateLimiter {
	return &RateLimiter{rate: rate, burst: burst, buckets: make(map[string]*bucket)}
}

// Allow reports whether a request for key may proceed, consuming a token.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &bucket{tokens: rl.burst - 1, last: now}
		return true
	}

	// Refill proportional to elapsed time, capped at burst.
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min(rl.burst, b.tokens+elapsed*rl.rate)
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
