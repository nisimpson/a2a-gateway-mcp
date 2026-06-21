package gateway

import "sync"

// HealthStatus represents the health state of an agent.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// HealthState holds the health tracking state for a single agent.
type HealthState struct {
	Status   HealthStatus
	Failures int // consecutive failure count
}

// HealthTracker manages per-agent health state.
// It is safe for concurrent access from multiple goroutines.
type HealthTracker struct {
	mu        sync.RWMutex
	agents    map[string]*HealthState // key: alias
	threshold int                     // failure threshold; 0 means tracking disabled
}

// NewHealthTracker creates a tracker with the given failure threshold.
// A threshold of 0 disables state transitions (all reads return unknown).
func NewHealthTracker(threshold int) *HealthTracker {
	return &HealthTracker{
		agents:    make(map[string]*HealthState),
		threshold: threshold,
	}
}

// RecordSuccess resets the failure count and sets status to healthy.
// No-op if threshold is 0 (disabled) or if alias has no entry.
func (h *HealthTracker) RecordSuccess(alias string) {
	if h.threshold == 0 {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.agents[alias]
	if !exists {
		return
	}

	state.Failures = 0
	state.Status = HealthStatusHealthy
}

// RecordFailure increments the failure count. If the new count reaches
// the threshold, sets status to unhealthy.
// No-op if threshold is 0 (disabled) or if alias has no entry.
func (h *HealthTracker) RecordFailure(alias string) {
	if h.threshold == 0 {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.agents[alias]
	if !exists {
		return
	}

	state.Failures++
	if state.Failures >= h.threshold {
		state.Status = HealthStatusUnhealthy
	}
}

// Get returns the current health state for an alias.
// Returns (unknown, 0) if the alias has no recorded state.
func (h *HealthTracker) Get(alias string) HealthState {
	h.mu.RLock()
	defer h.mu.RUnlock()

	state, exists := h.agents[alias]
	if !exists {
		return HealthState{Status: HealthStatusUnknown, Failures: 0}
	}

	return *state
}

// Reset sets an agent's state to (unknown, 0). Used on connect/re-connect.
// No-op if threshold is 0 (disabled).
func (h *HealthTracker) Reset(alias string) {
	if h.threshold == 0 {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.agents[alias] = &HealthState{
		Status:   HealthStatusUnknown,
		Failures: 0,
	}
}

// Delete removes all health state for an alias. Used on disconnect.
// No-op if threshold is 0 (disabled).
func (h *HealthTracker) Delete(alias string) {
	if h.threshold == 0 {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.agents, alias)
}

// IsEnabled returns true if health tracking is active (threshold > 0).
func (h *HealthTracker) IsEnabled() bool {
	return h.threshold > 0
}

// IsHealthy reports whether the specified alias is currently healthy.
// Returns true if the agent is not unhealthy (healthy, unknown, or untracked).
func (h *HealthTracker) IsHealthy(alias string) bool {
	state := h.Get(alias)
	return state.Status != HealthStatusUnhealthy
}

// GetStatus returns the health status string for the alias.
func (h *HealthTracker) GetStatus(alias string) string {
	return string(h.Get(alias).Status)
}

// GetFailures returns the consecutive failure count and whether the agent is unhealthy.
func (h *HealthTracker) GetFailures(alias string) (int, bool) {
	state := h.Get(alias)
	return state.Failures, state.Status == HealthStatusUnhealthy
}
