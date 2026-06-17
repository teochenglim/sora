// Package dedup deduplicates alerts using a sliding window keyed by a
// content fingerprint, backed by Redis (Valkey) with an in-memory fallback.
package dedup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/teochenglim/sora/internal/types"
)

// Fingerprint computes the dedup key for an alert.
func Fingerprint(a types.Alert) string {
	h := sha256.Sum256([]byte(a.AlertName + a.Instance + a.Namespace + a.Pod))
	return hex.EncodeToString(h[:])
}

// Deduper checks whether an alert has already been seen within the
// configured sliding window and tracks its occurrence count.
type Deduper interface {
	// CheckAndStore returns whether this fingerprint is a duplicate within
	// the window, and how many times it has now been seen (including this call).
	CheckAndStore(ctx context.Context, fingerprint string) (isDuplicate bool, occurrenceCount int, err error)
	// AcquireLock takes a distributed lock for the given key, returning a
	// release function. ok is false if the lock is already held.
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (release func(context.Context), ok bool, err error)
	Close() error
}

// RedisDeduper is the primary Deduper implementation backed by Redis/Valkey.
type RedisDeduper struct {
	client *redis.Client
	window time.Duration
}

// NewRedisDeduper creates a RedisDeduper. It pings the server eagerly so
// callers can fall back to NewMemoryDeduper if Redis is unavailable.
func NewRedisDeduper(ctx context.Context, addr, password string, window time.Duration) (*RedisDeduper, error) {
	client := redis.NewClient(&redis.Options{Addr: addr, Password: password})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connecting to redis at %s: %w", addr, err)
	}
	return &RedisDeduper{client: client, window: window}, nil
}

func (d *RedisDeduper) CheckAndStore(ctx context.Context, fingerprint string) (bool, int, error) {
	key := "sora:dedup:" + fingerprint
	count, err := d.client.Incr(ctx, key).Result()
	if err != nil {
		return false, 0, fmt.Errorf("incrementing dedup counter: %w", err)
	}
	if count == 1 {
		if err := d.client.Expire(ctx, key, d.window).Err(); err != nil {
			return false, 0, fmt.Errorf("setting dedup ttl: %w", err)
		}
	}
	return count > 1, int(count), nil
}

func (d *RedisDeduper) AcquireLock(ctx context.Context, key string, ttl time.Duration) (func(context.Context), bool, error) {
	lockKey := "sora:lock:" + key
	ok, err := d.client.SetNX(ctx, lockKey, "1", ttl).Result()
	if err != nil {
		return nil, false, fmt.Errorf("acquiring lock %s: %w", key, err)
	}
	release := func(rctx context.Context) { d.client.Del(rctx, lockKey) }
	return release, ok, nil
}

func (d *RedisDeduper) Close() error { return d.client.Close() }

// MemoryDeduper is a thread-safe, single-process fallback used when Redis
// is unavailable (and in demo mode).
type MemoryDeduper struct {
	mu     sync.Mutex
	window time.Duration
	counts map[string]*memEntry
	locks  map[string]time.Time
}

type memEntry struct {
	count     int
	expiresAt time.Time
}

// NewMemoryDeduper creates an in-memory Deduper.
func NewMemoryDeduper(window time.Duration) *MemoryDeduper {
	return &MemoryDeduper{
		window: window,
		counts: make(map[string]*memEntry),
		locks:  make(map[string]time.Time),
	}
}

func (d *MemoryDeduper) CheckAndStore(_ context.Context, fingerprint string) (bool, int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	e, exists := d.counts[fingerprint]
	if !exists || now.After(e.expiresAt) {
		e = &memEntry{count: 0, expiresAt: now.Add(d.window)}
		d.counts[fingerprint] = e
	}
	e.count++
	return e.count > 1, e.count, nil
}

func (d *MemoryDeduper) AcquireLock(_ context.Context, key string, ttl time.Duration) (func(context.Context), bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	if exp, held := d.locks[key]; held && now.Before(exp) {
		return func(context.Context) {}, false, nil
	}
	d.locks[key] = now.Add(ttl)
	release := func(_ context.Context) {
		d.mu.Lock()
		delete(d.locks, key)
		d.mu.Unlock()
	}
	return release, true, nil
}

func (d *MemoryDeduper) Close() error { return nil }
