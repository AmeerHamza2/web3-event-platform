package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisRateLimiter is a fixed-window limiter backed by Redis, so a fleet of
// gateway replicas enforces one shared limit per client instead of N separate
// in-process limits.
type RedisRateLimiter struct {
	client *redis.Client
	limit  int64
	window time.Duration
}

// NewRedisRateLimiter allows `limit` requests per `window` per key.
func NewRedisRateLimiter(addr string, limit int64, window time.Duration) *RedisRateLimiter {
	return &RedisRateLimiter{
		client: redis.NewClient(&redis.Options{Addr: addr}),
		limit:  limit,
		window: window,
	}
}

// Allow uses INCR + EXPIRE on a per-window key. It fails open: if Redis is
// unreachable, requests are allowed rather than dropping all traffic.
func (r *RedisRateLimiter) Allow(ctx context.Context, key string) bool {
	windowIdx := time.Now().Unix() / int64(r.window.Seconds())
	rkey := fmt.Sprintf("rl:%s:%d", key, windowIdx)

	count, err := r.client.Incr(ctx, rkey).Result()
	if err != nil {
		return true // fail open on Redis errors
	}
	if count == 1 {
		_ = r.client.Expire(ctx, rkey, r.window).Err()
	}
	return count <= r.limit
}

// Ping verifies connectivity at startup.
func (r *RedisRateLimiter) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}
