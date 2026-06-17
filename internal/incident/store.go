// Package incident persists Incident state across the remediation lifecycle.
package incident

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/teochenglim/sora/internal/types"
)

// Store persists and retrieves Incident records.
type Store interface {
	Save(ctx context.Context, inc types.Incident) error
	Get(ctx context.Context, id string) (types.Incident, bool, error)
	AppendAction(ctx context.Context, id string, action types.ActionRecord) error
	UpdateStatus(ctx context.Context, id, status string) error
	Close() error
}

const incidentTTL = 7 * 24 * time.Hour

// RedisStore is the primary Store implementation, backed by Redis/Valkey.
type RedisStore struct {
	client *redis.Client
}

// NewRedisStore creates a RedisStore, pinging eagerly so callers can fall
// back to NewMemoryStore if Redis is unavailable.
func NewRedisStore(ctx context.Context, addr, password string) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{Addr: addr, Password: password})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connecting to redis at %s: %w", addr, err)
	}
	return &RedisStore{client: client}, nil
}

func incidentKey(id string) string { return "sora:incident:" + id }

func (s *RedisStore) Save(ctx context.Context, inc types.Incident) error {
	inc.UpdatedAt = time.Now()
	data, err := json.Marshal(inc)
	if err != nil {
		return fmt.Errorf("marshalling incident %s: %w", inc.ID, err)
	}
	if err := s.client.Set(ctx, incidentKey(inc.ID), data, incidentTTL).Err(); err != nil {
		return fmt.Errorf("saving incident %s: %w", inc.ID, err)
	}
	return nil
}

func (s *RedisStore) Get(ctx context.Context, id string) (types.Incident, bool, error) {
	data, err := s.client.Get(ctx, incidentKey(id)).Bytes()
	if err == redis.Nil {
		return types.Incident{}, false, nil
	}
	if err != nil {
		return types.Incident{}, false, fmt.Errorf("getting incident %s: %w", id, err)
	}
	var inc types.Incident
	if err := json.Unmarshal(data, &inc); err != nil {
		return types.Incident{}, false, fmt.Errorf("unmarshalling incident %s: %w", id, err)
	}
	return inc, true, nil
}

func (s *RedisStore) AppendAction(ctx context.Context, id string, action types.ActionRecord) error {
	inc, ok, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("incident %s not found", id)
	}
	inc.Actions = append(inc.Actions, action)
	return s.Save(ctx, inc)
}

func (s *RedisStore) UpdateStatus(ctx context.Context, id, status string) error {
	inc, ok, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("incident %s not found", id)
	}
	inc.Status = status
	if status == types.StatusResolved {
		now := time.Now()
		inc.ResolvedAt = &now
	}
	return s.Save(ctx, inc)
}

func (s *RedisStore) Close() error { return s.client.Close() }

// MemoryStore is a thread-safe fallback used when Redis is unavailable
// (and in demo mode).
type MemoryStore struct {
	mu        sync.Mutex
	incidents map[string]types.Incident
}

// NewMemoryStore creates an in-memory Store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{incidents: make(map[string]types.Incident)}
}

func (s *MemoryStore) Save(_ context.Context, inc types.Incident) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inc.UpdatedAt = time.Now()
	s.incidents[inc.ID] = inc
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (types.Incident, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inc, ok := s.incidents[id]
	return inc, ok, nil
}

func (s *MemoryStore) AppendAction(ctx context.Context, id string, action types.ActionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inc, ok := s.incidents[id]
	if !ok {
		return fmt.Errorf("incident %s not found", id)
	}
	inc.Actions = append(inc.Actions, action)
	inc.UpdatedAt = time.Now()
	s.incidents[id] = inc
	return nil
}

func (s *MemoryStore) UpdateStatus(_ context.Context, id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inc, ok := s.incidents[id]
	if !ok {
		return fmt.Errorf("incident %s not found", id)
	}
	inc.Status = status
	inc.UpdatedAt = time.Now()
	if status == types.StatusResolved {
		now := time.Now()
		inc.ResolvedAt = &now
	}
	s.incidents[id] = inc
	return nil
}

func (s *MemoryStore) Close() error { return nil }
