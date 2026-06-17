// Package circuit implements a simple sliding-window circuit breaker used
// to protect the AI classification path and the Kubernetes API path.
package circuit

import (
	"sync"
	"time"
)

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// Breaker trips open when the failure rate over the last windowSize calls
// exceeds failureThreshold, and probes again after probeInterval.
type Breaker struct {
	mu               sync.Mutex
	name             string
	windowSize       int
	failureThreshold float64
	probeInterval    time.Duration

	results     []bool // true = success
	state       State
	openedAt    time.Time
	halfOpenUse bool
}

// New creates a Breaker. windowSize is the number of recent calls to
// consider, failureThreshold is the failure ratio (0.0-1.0) that trips it.
func New(name string, windowSize int, failureThreshold float64, probeInterval time.Duration) *Breaker {
	return &Breaker{
		name:             name,
		windowSize:       windowSize,
		failureThreshold: failureThreshold,
		probeInterval:    probeInterval,
		state:            Closed,
	}
}

// Allow reports whether a call should be attempted right now. When the
// breaker is open but the probe interval has elapsed, it moves to
// half-open and allows exactly one trial call.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case Closed:
		return true
	case HalfOpen:
		if b.halfOpenUse {
			return false
		}
		b.halfOpenUse = true
		return true
	case Open:
		if time.Since(b.openedAt) >= b.probeInterval {
			b.state = HalfOpen
			b.halfOpenUse = true
			return true
		}
		return false
	}
	return false
}

// Record reports the outcome of a call permitted by Allow.
func (b *Breaker) Record(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == HalfOpen {
		if success {
			b.state = Closed
			b.results = nil
		} else {
			b.state = Open
			b.openedAt = time.Now()
			b.results = nil
		}
		b.halfOpenUse = false
		return
	}

	b.results = append(b.results, success)
	if len(b.results) > b.windowSize {
		b.results = b.results[len(b.results)-b.windowSize:]
	}
	if len(b.results) < b.windowSize {
		return
	}

	failures := 0
	for _, r := range b.results {
		if !r {
			failures++
		}
	}
	if float64(failures)/float64(len(b.results)) >= b.failureThreshold {
		b.state = Open
		b.openedAt = time.Now()
	}
}

// State returns the current breaker state.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// MetricValue returns the numeric value used for sora_circuit_breaker_state.
func (b *Breaker) MetricValue() float64 {
	switch b.State() {
	case Open:
		return 1
	case HalfOpen:
		return 2
	default:
		return 0
	}
}
