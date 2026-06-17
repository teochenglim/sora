package remediator

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter caps the number of times a key may be used within a rolling
// window (used to enforce "max N restarts per service per hour").
type RateLimiter interface {
	// Allow increments the counter for key and reports whether it is still
	// within limit (i.e. the call should proceed).
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

// RedisRateLimiter is the primary RateLimiter, backed by Redis/Valkey.
type RedisRateLimiter struct {
	client *redis.Client
}

func NewRedisRateLimiter(client *redis.Client) *RedisRateLimiter {
	return &RedisRateLimiter{client: client}
}

func (r *RedisRateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	fullKey := "sora:ratelimit:" + key
	count, err := r.client.Incr(ctx, fullKey).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := r.client.Expire(ctx, fullKey, window).Err(); err != nil {
			return false, err
		}
	}
	return int(count) <= limit, nil
}

// MemoryRateLimiter is the in-memory fallback (and demo-mode default).
type MemoryRateLimiter struct {
	mu      sync.Mutex
	counts  map[string]*window
}

type window struct {
	count     int
	expiresAt time.Time
}

func NewMemoryRateLimiter() *MemoryRateLimiter {
	return &MemoryRateLimiter{counts: make(map[string]*window)}
}

func (r *MemoryRateLimiter) Allow(_ context.Context, key string, limit int, win time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	w, ok := r.counts[key]
	if !ok || now.After(w.expiresAt) {
		w = &window{expiresAt: now.Add(win)}
		r.counts[key] = w
	}
	w.count++
	return w.count <= limit, nil
}
