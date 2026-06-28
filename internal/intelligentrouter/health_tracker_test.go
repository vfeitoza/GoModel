package intelligentrouter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestTracker() *healthTracker {
	return &healthTracker{data: make(map[string][]healthEntry)}
}

func TestHealthTracker_NoDataReturnsHealthy(t *testing.T) {
	tracker := newTestTracker()
	score := tracker.score("openai/gpt-4o", time.Now(), healthDefaultWindow, healthDefaultHalfLife, healthDefaultPseudoCounts, healthDefaultCircuitBreaker)
	require.InDelta(t, 1.0, score, 0.001)
}

func TestHealthTracker_AllSuccessesIsHealthy(t *testing.T) {
	tracker := newTestTracker()
	now := time.Now()
	for i := 0; i < 10; i++ {
		tracker.record("m1", true, now.Add(-time.Duration(i)*time.Minute))
	}
	score := tracker.score("m1", now, healthDefaultWindow, healthDefaultHalfLife, healthDefaultPseudoCounts, healthDefaultCircuitBreaker)
	require.Greater(t, score, 0.9)
}

func TestHealthTracker_CircuitBreakerFiresAtThreshold(t *testing.T) {
	tracker := newTestTracker()
	now := time.Now()
	// 10 recent errors, no successes → raw error rate = 1.0 ≥ 0.9
	for i := 0; i < 10; i++ {
		tracker.record("bad-model", false, now.Add(-time.Duration(i)*time.Minute))
	}
	score := tracker.score("bad-model", now, healthDefaultWindow, healthDefaultHalfLife, healthDefaultPseudoCounts, healthDefaultCircuitBreaker)
	require.Equal(t, 0.0, score)
}

func TestHealthTracker_RecentErrorsWeighMoreThanOldOnes(t *testing.T) {
	tracker := newTestTracker()
	now := time.Now()
	// Several old successes and a few very recent errors.
	for i := 0; i < 8; i++ {
		tracker.record("m2", true, now.Add(-time.Duration(15+i)*time.Minute))
	}
	for i := 0; i < 3; i++ {
		tracker.record("m2", false, now.Add(-time.Duration(i)*time.Minute))
	}
	score := tracker.score("m2", now, healthDefaultWindow, healthDefaultHalfLife, healthDefaultPseudoCounts, healthDefaultCircuitBreaker)
	// Recent errors should pull score below a fully-healthy tracker.
	require.Less(t, score, 1.0)
	require.Greater(t, score, 0.0) // circuit breaker not tripped
}

func TestHealthTracker_EntriesOutsideWindowIgnored(t *testing.T) {
	tracker := newTestTracker()
	now := time.Now()
	// Add errors well outside the 20-minute window.
	for i := 0; i < 10; i++ {
		tracker.record("m3", false, now.Add(-time.Duration(30+i)*time.Minute))
	}
	score := tracker.score("m3", now, healthDefaultWindow, healthDefaultHalfLife, healthDefaultPseudoCounts, healthDefaultCircuitBreaker)
	require.InDelta(t, 1.0, score, 0.001, "out-of-window errors must not affect the score")
}
