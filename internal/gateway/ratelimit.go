package gateway

import (
	"context"
	"sync"
	"time"
)

// Limiter decides whether a request keyed by (client IP) may proceed.
// In-memory suits a single gateway; Redis shares limits across replicas.
type Limiter interface {
	Allow(ctx context.Context, key string) bool
}

// RateLimiter is a per-key token-bucket limiter local to one process.
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

// NewRateLimiter allows rate requests/sec per key with a burst allowance.
func NewRateLimiter(rate, burst float64) *RateLimiter {
	return &RateLimiter{rate: rate, burst: burst, buckets: make(map[string]*bucket)}
}

// Allow reports whether a request for key may proceed, consuming a token.
func (rl *RateLimiter) Allow(_ context.Context, key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &bucket{tokens: rl.burst - 1, last: now}
		return true
	}

	b.tokens = min(rl.burst, b.tokens+now.Sub(b.last).Seconds()*rl.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
