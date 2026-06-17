package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/teochenglim/sora/internal/circuit"
)

func TestBreaker_StaysClosedBelowThreshold(t *testing.T) {
	b := circuit.New("test", 10, 0.5, time.Minute)
	for i := 0; i < 4; i++ {
		assert.True(t, b.Allow())
		b.Record(false)
	}
	assert.Equal(t, circuit.Closed, b.State())
}

func TestBreaker_OpensAtThreshold(t *testing.T) {
	b := circuit.New("test", 4, 0.5, time.Minute)
	for i := 0; i < 4; i++ {
		assert.True(t, b.Allow())
		b.Record(i%2 == 0) // 50% failure
	}
	assert.Equal(t, circuit.Open, b.State())
	assert.False(t, b.Allow(), "open breaker must reject calls before probe interval")
}

func TestBreaker_HalfOpenProbeRecovers(t *testing.T) {
	b := circuit.New("test", 2, 0.5, 10*time.Millisecond)
	b.Allow()
	b.Record(false)
	b.Allow()
	b.Record(false)
	assert.Equal(t, circuit.Open, b.State())

	time.Sleep(20 * time.Millisecond)
	assert.True(t, b.Allow(), "after probe interval, one trial call should be allowed")
	b.Record(true)
	assert.Equal(t, circuit.Closed, b.State(), "successful probe should close the breaker")
}

func TestBreaker_HalfOpenProbeFailureReopens(t *testing.T) {
	b := circuit.New("test", 2, 0.5, 10*time.Millisecond)
	b.Allow()
	b.Record(false)
	b.Allow()
	b.Record(false)
	time.Sleep(20 * time.Millisecond)

	assert.True(t, b.Allow())
	b.Record(false)
	assert.Equal(t, circuit.Open, b.State())
}

func TestState_String(t *testing.T) {
	assert.Equal(t, "closed", circuit.Closed.String())
	assert.Equal(t, "open", circuit.Open.String())
	assert.Equal(t, "half-open", circuit.HalfOpen.String())
}

func TestBreaker_MetricValue(t *testing.T) {
	b := circuit.New("test", 2, 0.5, time.Minute)
	assert.Equal(t, float64(0), b.MetricValue())
	b.Allow()
	b.Record(false)
	b.Allow()
	b.Record(false)
	assert.Equal(t, float64(1), b.MetricValue())
}
