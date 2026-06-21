package registry

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimitConfig holds rate limit parameters for an agent.
type RateLimitConfig struct {
	RequestsPerSecond float64
	Burst             int
}

// IsDisabled returns true if the config represents "no rate limiting".
func (c *RateLimitConfig) IsDisabled() bool {
	return c.RequestsPerSecond <= 0 || c.Burst <= 0
}

// RateLimiterRegistry manages per-agent rate limiters.
// It is safe for concurrent access from multiple goroutines.
type RateLimiterRegistry struct {
	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
}

// NewRateLimiterRegistry creates an empty registry.
func NewRateLimiterRegistry() *RateLimiterRegistry {
	return &RateLimiterRegistry{
		limiters: make(map[string]*rate.Limiter),
	}
}

// Set creates or replaces the rate limiter for an alias.
// If rps <= 0 or burst <= 0, the entry is removed (no limit).
func (r *RateLimiterRegistry) Set(alias string, rps float64, burst int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if rps <= 0 || burst <= 0 {
		delete(r.limiters, alias)
		return
	}

	r.limiters[alias] = rate.NewLimiter(rate.Limit(rps), burst)
}

// Remove deletes the rate limiter for an alias.
func (r *RateLimiterRegistry) Remove(alias string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.limiters, alias)
}

// Allow checks if a request to the given alias is allowed.
// Returns true if allowed (token consumed), false if rate limited.
// If no limiter exists for the alias, returns true (no limit).
func (r *RateLimiterRegistry) Allow(alias string) bool {
	r.mu.RLock()
	limiter, exists := r.limiters[alias]
	r.mu.RUnlock()

	if !exists {
		return true
	}

	return limiter.Allow()
}

// Get returns the rate limit config for an alias for observability.
// Returns the configured rps, burst, and whether a limiter exists.
func (r *RateLimiterRegistry) Get(alias string) (rps float64, burst int, exists bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	limiter, exists := r.limiters[alias]
	if !exists {
		return 0, 0, false
	}

	return float64(limiter.Limit()), limiter.Burst(), true
}

// Len returns the number of active limiters.
func (r *RateLimiterRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.limiters)
}

// CheckRateLimit checks the rate limiter for an alias and returns a non-nil
// error if the request is rate limited. Returns nil if allowed or no limiter
// exists for the alias.
func (r *RateLimiterRegistry) CheckRateLimit(alias string) error {
	r.mu.RLock()
	limiter, exists := r.limiters[alias]
	r.mu.RUnlock()

	if !exists {
		return nil
	}

	reservation := limiter.Reserve()
	if reservation.Delay() == 0 {
		return nil
	}
	reservation.Cancel()
	waitTime := reservation.Delay().Round(time.Millisecond)
	return fmt.Errorf("agent %q has exceeded its rate limit; retry after %s", alias, waitTime)
}
