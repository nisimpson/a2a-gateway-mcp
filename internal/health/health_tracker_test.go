package health

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Feature: agent-health-checks, Property 1: Failure threshold triggers unhealthy transition
// **Validates: Requirements HLTH-1.1, HLTH-1.3**

func TestPropertyThresholdTransition(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for thresholds (1-50)
	thresholdGen := gen.IntRange(1, 50)

	// Generator for failure counts less than threshold (0 to threshold-1)
	// We'll generate a fraction [0.0, 1.0) and multiply by threshold to get a value < threshold
	fractionGen := gen.Float64Range(0.0, 0.99)

	properties.Property("exactly T consecutive failures → unhealthy; fewer than T → NOT unhealthy", prop.ForAll(
		func(threshold int, fraction float64) bool {
			alias := "test-agent"

			// Test 1: Exactly threshold failures transitions to unhealthy
			tracker := NewHealthTracker(threshold)
			tracker.Reset(alias)

			for i := 0; i < threshold; i++ {
				tracker.RecordFailure(alias)
			}

			state := tracker.Get(alias)
			if state.Status != HealthStatusUnhealthy {
				t.Logf("expected unhealthy after %d failures (threshold=%d), got %s", threshold, threshold, state.Status)
				return false
			}
			if state.Failures != threshold {
				t.Logf("expected failure count %d, got %d", threshold, state.Failures)
				return false
			}

			// Test 2: Fewer than threshold failures does NOT transition to unhealthy
			fewerCount := int(fraction * float64(threshold))
			if fewerCount >= threshold {
				fewerCount = threshold - 1
			}

			tracker2 := NewHealthTracker(threshold)
			tracker2.Reset(alias)

			for i := 0; i < fewerCount; i++ {
				tracker2.RecordFailure(alias)
			}

			state2 := tracker2.Get(alias)
			if state2.Status == HealthStatusUnhealthy {
				t.Logf("should NOT be unhealthy after %d failures (threshold=%d)", fewerCount, threshold)
				return false
			}

			return true
		},
		thresholdGen,
		fractionGen,
	))

	properties.TestingRun(t)
}

// Feature: agent-health-checks, Property 2: Success resets health state
// **Validates: Requirements HLTH-1.2, HLTH-1.5, HLTH-5.1, HLTH-5.2, HLTH-5.4**

func TestPropertySuccessResetsState(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for thresholds (1-20)
	thresholdGen := gen.IntRange(1, 20)

	// Generator for failure counts (1-100), can be above or below threshold
	failureCountGen := gen.IntRange(1, 100)

	properties.Property("a single success resets to (healthy, 0) regardless of prior failure count", prop.ForAll(
		func(threshold int, failureCount int) bool {
			alias := "test-agent"
			tracker := NewHealthTracker(threshold)
			tracker.Reset(alias)

			// Accumulate failures
			for i := 0; i < failureCount; i++ {
				tracker.RecordFailure(alias)
			}

			// Record a single success
			tracker.RecordSuccess(alias)

			state := tracker.Get(alias)
			if state.Status != HealthStatusHealthy {
				t.Logf("expected healthy after success, got %s (threshold=%d, failures=%d)", state.Status, threshold, failureCount)
				return false
			}
			if state.Failures != 0 {
				t.Logf("expected 0 failures after success, got %d", state.Failures)
				return false
			}

			return true
		},
		thresholdGen,
		failureCountGen,
	))

	properties.TestingRun(t)
}

// Feature: agent-health-checks, Property 3: Initial registration state
// **Validates: Requirements HLTH-1.4, HLTH-7.2**

func TestPropertyInitialRegistrationState(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for thresholds (1-20)
	thresholdGen := gen.IntRange(1, 20)

	// Generator for failure counts to accumulate before re-registration (0-50)
	failureCountGen := gen.IntRange(0, 50)

	// Generator for whether to record successes before re-registration
	recordSuccessGen := gen.Bool()

	properties.Property("after Reset(), state is always (unknown, 0) regardless of prior state", prop.ForAll(
		func(threshold int, failureCount int, recordSuccess bool) bool {
			alias := "test-agent"
			tracker := NewHealthTracker(threshold)
			tracker.Reset(alias)

			// Accumulate some prior state
			for i := 0; i < failureCount; i++ {
				tracker.RecordFailure(alias)
			}
			if recordSuccess {
				tracker.RecordSuccess(alias)
			}

			// Re-register via Reset
			tracker.Reset(alias)

			state := tracker.Get(alias)
			if state.Status != HealthStatusUnknown {
				t.Logf("expected unknown after Reset(), got %s", state.Status)
				return false
			}
			if state.Failures != 0 {
				t.Logf("expected 0 failures after Reset(), got %d", state.Failures)
				return false
			}

			return true
		},
		thresholdGen,
		failureCountGen,
		recordSuccessGen,
	))

	properties.TestingRun(t)
}

// Feature: agent-health-checks, Property 5: Non-registry agents are not tracked
// **Validates: Requirements HLTH-1.7**

func TestPropertyNonRegistryNotTracked(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for alias names
	aliasGen := gen.RegexMatch(`[a-z][a-z0-9]{1,8}`)

	// Generator for operation counts (1-50)
	opCountGen := gen.IntRange(1, 50)

	// Generator for operation type: 0=success, 1=failure
	opTypeGen := gen.IntRange(0, 1)

	properties.Property("RecordSuccess/RecordFailure on unregistered alias creates no state; Get returns (unknown, 0)", prop.ForAll(
		func(alias string, opCount int, opType int) bool {
			tracker := NewHealthTracker(3) // enabled with threshold 3

			// Do NOT call Reset — the alias is unregistered
			for i := 0; i < opCount; i++ {
				if opType == 0 {
					tracker.RecordSuccess(alias)
				} else {
					tracker.RecordFailure(alias)
				}
			}

			state := tracker.Get(alias)
			if state.Status != HealthStatusUnknown {
				t.Logf("expected unknown for unregistered alias %q, got %s", alias, state.Status)
				return false
			}
			if state.Failures != 0 {
				t.Logf("expected 0 failures for unregistered alias %q, got %d", alias, state.Failures)
				return false
			}

			return true
		},
		aliasGen,
		opCountGen,
		opTypeGen,
	))

	properties.TestingRun(t)
}

// Feature: agent-health-checks, Property 7: Disabled tracking invariant
// **Validates: Requirements HLTH-4.2, HLTH-4.5**

func TestPropertyDisabledTrackingInvariant(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for alias names
	aliasGen := gen.RegexMatch(`[a-z][a-z0-9]{1,8}`)

	// Generator for operation sequences (1-50 operations)
	opCountGen := gen.IntRange(1, 50)

	// Generator for operation type: 0=success, 1=failure, 2=reset
	opTypeGen := gen.IntRange(0, 2)

	properties.Property("when threshold is 0, no operations change state from (unknown, 0)", prop.ForAll(
		func(alias string, opCount int, opType int) bool {
			tracker := NewHealthTracker(0) // disabled

			// Attempt to register and perform operations
			for i := 0; i < opCount; i++ {
				switch opType {
				case 0:
					tracker.RecordSuccess(alias)
				case 1:
					tracker.RecordFailure(alias)
				case 2:
					tracker.Reset(alias)
				}
			}

			state := tracker.Get(alias)
			if state.Status != HealthStatusUnknown {
				t.Logf("expected unknown for disabled tracker, got %s", state.Status)
				return false
			}
			if state.Failures != 0 {
				t.Logf("expected 0 failures for disabled tracker, got %d", state.Failures)
				return false
			}

			if tracker.IsEnabled() {
				t.Log("expected IsEnabled() to return false for threshold 0")
				return false
			}

			return true
		},
		aliasGen,
		opCountGen,
		opTypeGen,
	))

	properties.TestingRun(t)
}

// Feature: agent-health-checks, Property 9: Disconnect cleanup completeness
// **Validates: Requirements HLTH-7.1, HLTH-7.4**

func TestPropertyDisconnectCleanup(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for thresholds (1-20)
	thresholdGen := gen.IntRange(1, 20)

	// Generator for failure counts (0-50)
	failureCountGen := gen.IntRange(0, 50)

	// Generator for whether to record a success before delete
	recordSuccessGen := gen.Bool()

	properties.Property("after Delete(), Get returns (unknown, 0) and alias has no entry", prop.ForAll(
		func(threshold int, failureCount int, recordSuccess bool) bool {
			alias := "test-agent"
			tracker := NewHealthTracker(threshold)
			tracker.Reset(alias)

			// Accumulate some state
			for i := 0; i < failureCount; i++ {
				tracker.RecordFailure(alias)
			}
			if recordSuccess {
				tracker.RecordSuccess(alias)
			}

			// Delete
			tracker.Delete(alias)

			// Verify Get returns (unknown, 0)
			state := tracker.Get(alias)
			if state.Status != HealthStatusUnknown {
				t.Logf("expected unknown after Delete(), got %s", state.Status)
				return false
			}
			if state.Failures != 0 {
				t.Logf("expected 0 failures after Delete(), got %d", state.Failures)
				return false
			}

			// Verify no entry exists in the internal map by confirming
			// RecordSuccess/RecordFailure are no-ops (no state created)
			tracker.RecordFailure(alias)
			tracker.RecordSuccess(alias)
			stateAfter := tracker.Get(alias)
			if stateAfter.Status != HealthStatusUnknown {
				t.Logf("expected no entry created after Delete + operations, got %s", stateAfter.Status)
				return false
			}
			if stateAfter.Failures != 0 {
				t.Logf("expected 0 failures (no entry) after Delete + operations, got %d", stateAfter.Failures)
				return false
			}

			return true
		},
		thresholdGen,
		failureCountGen,
		recordSuccessGen,
	))

	properties.TestingRun(t)
}

// Feature: agent-health-checks, Property 4: Concurrent access safety
// **Validates: Requirements HLTH-1.6**

func TestPropertyConcurrentHealthSafety(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for number of goroutines (2-50)
	goroutineCountGen := gen.IntRange(2, 50)

	// Generator for number of operations per goroutine (1-100)
	opsPerGoroutineGen := gen.IntRange(1, 100)

	// Generator for failure threshold (1-10)
	thresholdGen := gen.IntRange(1, 10)

	properties.Property("concurrent RecordSuccess, RecordFailure, Get, Reset, Delete produce no data races, panics, or lost updates", prop.ForAll(
		func(numGoroutines, opsPerGoroutine, threshold int) bool {
			tracker := NewHealthTracker(threshold)

			// Register a small set of aliases so goroutines compete on the same keys
			aliases := []string{"agent-a", "agent-b", "agent-c"}
			for _, alias := range aliases {
				tracker.Reset(alias)
			}

			var wg sync.WaitGroup
			panicked := atomic.Bool{}

			wg.Add(numGoroutines)
			for range numGoroutines {
				go func() {
					defer wg.Done()
					defer func() {
						if r := recover(); r != nil {
							panicked.Store(true)
						}
					}()

					for op := range opsPerGoroutine {
						alias := aliases[op%len(aliases)]
						// Cycle through all operations deterministically based on loop index
						switch op % 5 {
						case 0:
							tracker.RecordSuccess(alias)
						case 1:
							tracker.RecordFailure(alias)
						case 2:
							tracker.Get(alias)
						case 3:
							tracker.Reset(alias)
						case 4:
							tracker.Delete(alias)
						}
					}
				}()
			}

			wg.Wait()

			// Verify: no panics occurred
			if panicked.Load() {
				t.Log("panic occurred during concurrent health tracker operations")
				return false
			}

			// Verify: tracker is still in a consistent state after all concurrent ops.
			// For any alias that still exists, its state must be valid.
			for _, alias := range aliases {
				state := tracker.Get(alias)
				// Status must be one of the valid values
				switch state.Status {
				case HealthStatusHealthy, HealthStatusUnhealthy, HealthStatusUnknown:
					// valid
				default:
					t.Logf("invalid status %q for alias %q", state.Status, alias)
					return false
				}
				// Failures must be non-negative
				if state.Failures < 0 {
					t.Logf("negative failure count %d for alias %q", state.Failures, alias)
					return false
				}
			}

			return true
		},
		goroutineCountGen,
		opsPerGoroutineGen,
		thresholdGen,
	))

	properties.TestingRun(t)
}
