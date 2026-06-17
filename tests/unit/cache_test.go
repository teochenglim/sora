package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/teochenglim/sora/internal/cache"
	"github.com/teochenglim/sora/internal/types"
)

func TestContextCache_StoresAndRetrievesSimilarAlerts(t *testing.T) {
	c := cache.New(time.Minute)
	c.Store(types.Alert{AlertName: "HighCPU", Namespace: "default", Pod: "pod-1"})
	c.Store(types.Alert{AlertName: "HighCPU", Namespace: "default", Pod: "pod-2"})

	similar := c.Similar("HighCPU", "default")
	assert.Len(t, similar, 2)
}

func TestContextCache_IsolatesByNamespace(t *testing.T) {
	c := cache.New(time.Minute)
	c.Store(types.Alert{AlertName: "HighCPU", Namespace: "default", Pod: "pod-1"})
	c.Store(types.Alert{AlertName: "HighCPU", Namespace: "staging", Pod: "pod-2"})

	assert.Len(t, c.Similar("HighCPU", "default"), 1)
	assert.Len(t, c.Similar("HighCPU", "staging"), 1)
}

func TestContextCache_ExpiresAfterTTL(t *testing.T) {
	c := cache.New(20 * time.Millisecond)
	c.Store(types.Alert{AlertName: "HighCPU", Namespace: "default", Pod: "pod-1"})
	time.Sleep(40 * time.Millisecond)

	assert.Empty(t, c.Similar("HighCPU", "default"))
}

func TestContextCache_CapsAtFiveEntries(t *testing.T) {
	c := cache.New(time.Minute)
	for i := 0; i < 8; i++ {
		c.Store(types.Alert{AlertName: "HighCPU", Namespace: "default", Pod: "pod"})
	}
	assert.Len(t, c.Similar("HighCPU", "default"), 5)
}
