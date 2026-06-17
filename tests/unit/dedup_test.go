package unit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/dedup"
	"github.com/teochenglim/sora/internal/types"
)

func TestFingerprint_DeterministicAndDistinct(t *testing.T) {
	a := types.Alert{AlertName: "CrashLoopBackOff", Instance: "10.0.0.5", Namespace: "default", Pod: "worker-1"}
	b := a
	b.Pod = "worker-2"

	assert.Equal(t, dedup.Fingerprint(a), dedup.Fingerprint(a), "fingerprint must be deterministic")
	assert.NotEqual(t, dedup.Fingerprint(a), dedup.Fingerprint(b), "different pods must fingerprint differently")
}

func TestMemoryDeduper_FirstSeenIsNotDuplicate(t *testing.T) {
	d := dedup.NewMemoryDeduper(5 * time.Minute)
	isDup, count, err := d.CheckAndStore(context.Background(), "fp-1")
	require.NoError(t, err)
	assert.False(t, isDup)
	assert.Equal(t, 1, count)
}

func TestMemoryDeduper_SubsequentCallsAreDuplicates(t *testing.T) {
	d := dedup.NewMemoryDeduper(5 * time.Minute)
	ctx := context.Background()

	_, _, err := d.CheckAndStore(ctx, "fp-1")
	require.NoError(t, err)

	isDup, count, err := d.CheckAndStore(ctx, "fp-1")
	require.NoError(t, err)
	assert.True(t, isDup)
	assert.Equal(t, 2, count)
}

func TestMemoryDeduper_WindowExpiry(t *testing.T) {
	d := dedup.NewMemoryDeduper(50 * time.Millisecond)
	ctx := context.Background()

	_, _, err := d.CheckAndStore(ctx, "fp-1")
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	isDup, count, err := d.CheckAndStore(ctx, "fp-1")
	require.NoError(t, err)
	assert.False(t, isDup, "after window expiry, occurrence should reset")
	assert.Equal(t, 1, count)
}

func TestMemoryDeduper_ConcurrentAccess(t *testing.T) {
	d := dedup.NewMemoryDeduper(5 * time.Minute)
	ctx := context.Background()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, _, err := d.CheckAndStore(ctx, "fp-concurrent")
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	_, count, err := d.CheckAndStore(ctx, "fp-concurrent")
	require.NoError(t, err)
	assert.Equal(t, goroutines+1, count, "every concurrent call must be counted exactly once")
}

func TestMemoryDeduper_DistributedLock(t *testing.T) {
	d := dedup.NewMemoryDeduper(5 * time.Minute)
	ctx := context.Background()

	release, ok, err := d.AcquireLock(ctx, "incident-1", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	_, ok2, err := d.AcquireLock(ctx, "incident-1", time.Minute)
	require.NoError(t, err)
	assert.False(t, ok2, "lock should not be acquirable twice while held")

	release(ctx)

	_, ok3, err := d.AcquireLock(ctx, "incident-1", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok3, "lock should be acquirable again after release")
}
