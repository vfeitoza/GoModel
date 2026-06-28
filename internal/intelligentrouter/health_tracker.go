package intelligentrouter

import (
	"math"
	"sync"
	"time"
)

const (
	healthDefaultWindow         = 20 * time.Minute
	healthDefaultHalfLife       = 5 * time.Minute
	healthDefaultPseudoCounts   = 2.0
	healthDefaultCircuitBreaker = 0.9
	healthMaxEntries            = 500 // per-model cap to bound memory
)

type healthEntry struct {
	success   bool
	timestamp time.Time
}

type healthTracker struct {
	mu   sync.Mutex
	data map[string][]healthEntry
}

var defaultHealthTracker = &healthTracker{data: make(map[string][]healthEntry)}

// RecordHealth records the outcome of a provider call for a qualified model ID.
func RecordHealth(modelID string, success bool) {
	defaultHealthTracker.record(modelID, success, time.Now())
}

// ModelHealthScore returns a health score in [0.0, 1.0] for a model using
// exponential decay. Returns 1.0 when there is no recent data (new model).
// A score of 0.0 means the circuit breaker fired (weighted error rate >= threshold).
func ModelHealthScore(modelID string, now time.Time, window, halfLife time.Duration, pseudoCounts, circuitBreaker float64) float64 {
	return defaultHealthTracker.score(modelID, now, window, halfLife, pseudoCounts, circuitBreaker)
}

func (t *healthTracker) record(modelID string, success bool, now time.Time) {
	if t == nil || modelID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := append(t.data[modelID], healthEntry{success: success, timestamp: now})
	if len(entries) > healthMaxEntries {
		entries = entries[len(entries)-healthMaxEntries:]
	}
	t.data[modelID] = entries
}

// score computes the weighted error rate with exponential decay and applies
// Bayesian smoothing. The raw rate is used for the circuit breaker check to
// avoid pseudoCounts masking a model that is clearly broken.
func (t *healthTracker) score(modelID string, now time.Time, window, halfLife time.Duration, pseudoCounts, circuitBreaker float64) float64 {
	if t == nil || modelID == "" {
		return 1.0
	}
	if window <= 0 {
		window = healthDefaultWindow
	}
	if halfLife <= 0 {
		halfLife = healthDefaultHalfLife
	}
	if pseudoCounts <= 0 {
		pseudoCounts = healthDefaultPseudoCounts
	}
	if circuitBreaker <= 0 || circuitBreaker > 1 {
		circuitBreaker = healthDefaultCircuitBreaker
	}

	t.mu.Lock()
	entries := t.data[modelID]
	t.mu.Unlock()

	cutoff := now.Add(-window)
	halfLifeNs := float64(halfLife.Nanoseconds())

	var weightedErrors, weightedTotal float64
	for _, e := range entries {
		if e.timestamp.Before(cutoff) {
			continue
		}
		ageNs := float64(now.Sub(e.timestamp).Nanoseconds())
		w := math.Exp(-math.Ln2 * ageNs / halfLifeNs)
		weightedTotal += w
		if !e.success {
			weightedErrors += w
		}
	}

	if weightedTotal == 0 {
		return 1.0 // no data → healthy (active exploration)
	}

	rawRate := weightedErrors / weightedTotal
	if rawRate >= circuitBreaker {
		return 0.0 // circuit breaker tripped
	}
	smoothedRate := weightedErrors / (weightedTotal + pseudoCounts)
	return math.Max(0, 1.0-smoothedRate)
}
