package unit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/teochenglim/sora/internal/dedup"
	"github.com/teochenglim/sora/internal/incident"
)

// These tests don't require a running Redis: they assert that SORA fails
// fast and clearly when Redis is unreachable, which is the documented
// trigger for falling back to in-memory dedup/incident stores (see
// cmd/sora/main.go buildPersistence).

func TestNewRedisDeduper_UnreachableReturnsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := dedup.NewRedisDeduper(ctx, "127.0.0.1:1", "", 5*time.Minute)
	assert.Error(t, err)
}

func TestNewRedisStore_UnreachableReturnsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := incident.NewRedisStore(ctx, "127.0.0.1:1", "")
	assert.Error(t, err)
}
