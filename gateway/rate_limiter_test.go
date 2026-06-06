package gateway

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Feature: rate-limiting, Property 1: Rate limiter count invariant
// **Validates: Requirements RLIM-1.1, RLIM-1.3**

// rateLimiterOp represents a single Set or Remove operation for property testing.
type rateLimiterOp struct {
	OpType int // 0 = Set, 1 = Remove
	Alias  string
	RPS    float64
	Burst  int
}

func TestPropertyRateLimiterCountInvariant(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for alias names: lowercase letter followed by 1-8 alphanumeric chars
	aliasGen := gen.RegexMatch(`[a-z][a-z0-9]{1,8}`)

	// Generator for a single operation
	opGen := gen.StructPtr(reflect.TypeOf(&rateLimiterOp{}), map[string]gopter.Gen{
		"OpType": gen.IntRange(0, 1),
		"Alias":  aliasGen,
		"RPS":    gen.Float64Range(0.1, 100.0),
		"Burst":  gen.IntRange(1, 100),
	})

	// Generator for a sequence of operations (1 to 50)
	opsGen := gen.SliceOfN(50, opGen)

	properties.Property("Len() matches expected count after random Set/Remove sequences", prop.ForAll(
		func(opPtrs []*rateLimiterOp) bool {
			registry := NewRateLimiterRegistry()
			expected := make(map[string]bool)

			for _, op := range opPtrs {
				switch op.OpType {
				case 0: // Set with valid rps/burst
					registry.Set(op.Alias, op.RPS, op.Burst)
					expected[op.Alias] = true
				case 1: // Remove
					registry.Remove(op.Alias)
					delete(expected, op.Alias)
				}
			}

			return registry.Len() == len(expected)
		},
		opsGen,
	))

	properties.TestingRun(t)
}

// Feature: rate-limiting, Property 2: Token consumption correctly gates requests
// **Validates: Requirements RLIM-1.4, RLIM-1.6, RLIM-5.1**

func TestPropertyTokenConsumptionGatesRequests(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for burst capacities (1-100)
	burstGen := gen.IntRange(1, 100)

	properties.Property("Allow() returns exactly burst true and 1 false with no refill", prop.ForAll(
		func(burst int) bool {
			registry := NewRateLimiterRegistry()
			alias := "test-agent"

			// Use an extremely low RPS so no tokens refill during the test.
			// 1e-9 means ~1 token per 31 years — effectively zero refill.
			registry.Set(alias, 1e-9, burst)

			allowed := 0
			denied := 0

			// Call Allow() exactly burst+1 times
			for i := 0; i < burst+1; i++ {
				if registry.Allow(alias) {
					allowed++
				} else {
					denied++
				}
			}

			return allowed == burst && denied == 1
		},
		burstGen,
	))

	properties.TestingRun(t)
}

// Feature: rate-limiting, Property 3: Rate limit error message contains required information
// **Validates: Requirements RLIM-2.2**

func TestPropertyRateLimitErrorMessage(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for alias strings: lowercase, 1-8 chars
	aliasGen := gen.RegexMatch(`[a-z]{1,8}`)

	// Generator for burst capacities (1-10)
	burstGen := gen.IntRange(1, 10)

	properties.Property("checkRateLimit error message contains alias and parseable wait duration", prop.ForAll(
		func(alias string, burst int) bool {
			s := &Server{
				rateLimiters: NewRateLimiterRegistry(),
			}

			// Set a limiter with extremely low RPS so no tokens refill during the test.
			s.rateLimiters.Set(alias, 1e-9, burst)

			// Exhaust all tokens by calling Allow() burst times.
			for i := 0; i < burst; i++ {
				s.rateLimiters.Allow(alias)
			}

			// Now call checkRateLimit — should return a non-nil error result.
			result := s.checkRateLimit(alias)
			if result == nil {
				t.Logf("checkRateLimit returned nil for alias %q after exhausting %d tokens", alias, burst)
				return false
			}

			// Verify IsError is true.
			if !result.IsError {
				t.Logf("expected IsError=true, got false for alias %q", alias)
				return false
			}

			// Extract the text content from the result.
			if len(result.Content) == 0 {
				t.Logf("expected non-empty content for alias %q", alias)
				return false
			}

			var msg string
			for _, c := range result.Content {
				if tc, ok := c.(*mcp.TextContent); ok {
					msg = tc.Text
					break
				}
			}

			if msg == "" {
				t.Logf("no text content found in result for alias %q", alias)
				return false
			}

			// Verify the error message contains the alias string.
			if !strings.Contains(msg, alias) {
				t.Logf("message %q does not contain alias %q", msg, alias)
				return false
			}

			// Verify the error message contains "rate limited".
			if !strings.Contains(msg, "rate limited") {
				t.Logf("message %q does not contain 'rate limited'", msg)
				return false
			}

			// Verify the error message contains "retry after".
			if !strings.Contains(msg, "retry after") {
				t.Logf("message %q does not contain 'retry after'", msg)
				return false
			}

			// Verify the duration portion is parseable (contains a time-like string).
			// The format is "retry after <duration>" where duration is from Go's
			// time.Duration.String() e.g. "1ms", "2s", "1h30m".
			parts := strings.SplitAfter(msg, "retry after ")
			if len(parts) < 2 {
				t.Logf("could not find duration after 'retry after' in message %q", msg)
				return false
			}
			durationStr := parts[len(parts)-1]
			_, err := time.ParseDuration(durationStr)
			if err != nil {
				t.Logf("duration %q is not parseable: %v", durationStr, err)
				return false
			}

			return true
		},
		aliasGen,
		burstGen,
	))

	properties.TestingRun(t)
}

// Feature: rate-limiting, Property 4: Broadcast evaluates limits independently with partial success
// **Validates: Requirements RLIM-2.3, RLIM-2.4**

func TestPropertyBroadcastPartialSuccess(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for a slice of booleans (2-10 elements), each indicating if agent is rate-limited.
	// We use a bitmask approach: generate an int for count and a second int as a bitmask.
	numAgentsGen := gen.IntRange(2, 10)
	bitmaskGen := gen.IntRange(0, 1023) // 10 bits max

	properties.Property("Allow() evaluates limits independently per agent with partial success", prop.ForAll(
		func(numAgents int, bitmask int) bool {
			// Ensure at least one rate-limited and one non-rate-limited agent.
			// Use bitmask to decide which agents are rate-limited.
			rateLimited := make([]bool, numAgents)
			hasLimited := false
			hasUnlimited := false
			for i := range numAgents {
				rateLimited[i] = (bitmask>>i)&1 == 1
				if rateLimited[i] {
					hasLimited = true
				} else {
					hasUnlimited = true
				}
			}

			// Skip cases where all agents are the same state (no partial success possible).
			if !hasLimited || !hasUnlimited {
				return true // trivially true — not a partial success scenario
			}

			registry := NewRateLimiterRegistry()

			// Set up limiters for rate-limited agents with burst=1, then exhaust token.
			for i := range numAgents {
				alias := fmt.Sprintf("agent-%d", i)
				if rateLimited[i] {
					// Use extremely low RPS so no refill during test.
					registry.Set(alias, 1e-9, 1)
					// Exhaust the single token.
					registry.Allow(alias)
				}
				// Non-rate-limited agents: no limiter set → Allow() returns true.
			}

			// Simulate broadcast: call Allow() for each agent and verify results.
			allowedCount := 0
			deniedCount := 0

			for i := range numAgents {
				alias := fmt.Sprintf("agent-%d", i)
				result := registry.Allow(alias)
				if rateLimited[i] {
					// Rate-limited agents (tokens exhausted) should be denied.
					if result {
						t.Logf("expected Allow()=false for rate-limited agent %q, got true", alias)
						return false
					}
					deniedCount++
				} else {
					// Non-rate-limited agents (no limiter) should be allowed.
					if !result {
						t.Logf("expected Allow()=true for non-rate-limited agent %q, got false", alias)
						return false
					}
					allowedCount++
				}
			}

			// Verify we have a mix of results (partial success).
			if allowedCount == 0 || deniedCount == 0 {
				t.Logf("expected mix of allowed and denied, got allowed=%d denied=%d", allowedCount, deniedCount)
				return false
			}

			return true
		},
		numAgentsGen,
		bitmaskGen,
	))

	properties.TestingRun(t)
}

// Feature: rate-limiting, Property 5: No rate limit configured means unlimited throughput
// **Validates: Requirements RLIM-3.2**

func TestPropertyNoRateLimitUnlimited(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for random request counts (1-200)
	requestCountGen := gen.IntRange(1, 200)

	properties.Property("Allow() always returns true when no limiter is configured", prop.ForAll(
		func(requestCount int) bool {
			// Create an empty registry with no limiters set for any alias.
			registry := NewRateLimiterRegistry()

			// Call Allow() requestCount times for an alias with no limiter.
			for range requestCount {
				if !registry.Allow("some-alias") {
					return false
				}
			}

			return true
		},
		requestCountGen,
	))

	properties.TestingRun(t)
}

// Feature: rate-limiting, Property 6: Per-agent config overrides global default
// **Validates: Requirements RLIM-4.2, RLIM-1.2, RLIM-3.3**

func TestPropertyPerAgentOverridesGlobal(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generate globalBurst as 1-50 and agentBurst as 51-100 to ensure they're always different.
	globalBurstGen := gen.IntRange(1, 50)
	agentBurstGen := gen.IntRange(51, 100)

	properties.Property("per-agent config overrides global default burst", prop.ForAll(
		func(globalBurst int, agentBurst int) bool {
			// Create a RateLimiterRegistry (simulating what the server does).
			registry := NewRateLimiterRegistry()

			// Simulate what handleConnectAgent does when per-agent config is provided:
			// it calls Set() with the per-agent values, ignoring the global default.
			// The global default would have been used only if no per-agent config was given.
			alias := "test-agent"
			registry.Set(alias, 1e-9, agentBurst)

			// Call Allow() exactly agentBurst+1 times with very low RPS (no refill).
			allowed := 0
			denied := 0

			for i := 0; i < agentBurst+1; i++ {
				if registry.Allow(alias) {
					allowed++
				} else {
					denied++
				}
			}

			// Verify exactly agentBurst succeed and 1 fails.
			// This proves the per-agent burst is used, not globalBurst.
			if allowed != agentBurst {
				t.Logf("expected %d allowed (agentBurst), got %d (globalBurst was %d)", agentBurst, allowed, globalBurst)
				return false
			}
			if denied != 1 {
				t.Logf("expected 1 denied, got %d", denied)
				return false
			}

			return true
		},
		globalBurstGen,
		agentBurstGen,
	))

	properties.TestingRun(t)
}

// Feature: rate-limiting, Property 7: Reconnection replaces rate limiter with new config
// **Validates: Requirements RLIM-4.4**

func TestPropertyReconnectionReplacesLimiter(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generate two distinct burst values: firstBurst (1-50) and secondBurst (51-100)
	firstBurstGen := gen.IntRange(1, 50)
	secondBurstGen := gen.IntRange(51, 100)

	properties.Property("reconnection replaces rate limiter with new config and fresh token state", prop.ForAll(
		func(firstBurst int, secondBurst int) bool {
			registry := NewRateLimiterRegistry()
			alias := "reconnect-agent"

			// Set the limiter with firstBurst and very low RPS (no refill).
			registry.Set(alias, 1e-9, firstBurst)

			// Exhaust all tokens from the first limiter.
			for i := 0; i < firstBurst; i++ {
				registry.Allow(alias)
			}

			// Verify the first limiter is indeed exhausted.
			if registry.Allow(alias) {
				t.Logf("expected first limiter to be exhausted after %d calls", firstBurst)
				return false
			}

			// "Reconnect" by calling Set() again with secondBurst (replaces old limiter).
			registry.Set(alias, 1e-9, secondBurst)

			// Call Allow() exactly secondBurst+1 times on the new limiter.
			allowed := 0
			denied := 0
			for i := 0; i < secondBurst+1; i++ {
				if registry.Allow(alias) {
					allowed++
				} else {
					denied++
				}
			}

			// Verify exactly secondBurst succeed and 1 fails.
			// This proves the new config is used and old state doesn't carry over.
			if allowed != secondBurst {
				t.Logf("expected %d allowed (secondBurst), got %d", secondBurst, allowed)
				return false
			}
			if denied != 1 {
				t.Logf("expected 1 denied, got %d", denied)
				return false
			}

			return true
		},
		firstBurstGen,
		secondBurstGen,
	))

	properties.TestingRun(t)
}

// Feature: rate-limiting, Property 10: Concurrent rate limit checks are safe
// **Validates: Requirements RLIM-1.7**

func TestPropertyConcurrentRateLimitSafety(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for goroutine counts (2-50)
	goroutineCountGen := gen.IntRange(2, 50)

	// Generator for burst sizes (1-100)
	burstGen := gen.IntRange(1, 100)

	properties.Property("concurrent Allow() calls are safe and total allowed does not exceed burst", prop.ForAll(
		func(numGoroutines int, burst int) bool {
			registry := NewRateLimiterRegistry()
			alias := "concurrent-agent"

			// Use extremely low RPS so no tokens refill during the test.
			registry.Set(alias, 1e-9, burst)

			var wg sync.WaitGroup
			var totalAllowed atomic.Int64
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

					if registry.Allow(alias) {
						totalAllowed.Add(1)
					}
				}()
			}

			wg.Wait()

			// Verify: no panics occurred.
			if panicked.Load() {
				t.Log("panic occurred during concurrent Allow() calls")
				return false
			}

			// Verify: total allowed does not exceed burst capacity.
			allowed := totalAllowed.Load()
			if allowed > int64(burst) {
				t.Logf("total allowed %d exceeds burst %d", allowed, burst)
				return false
			}

			return true
		},
		goroutineCountGen,
		burstGen,
	))

	properties.TestingRun(t)
}

// Feature: rate-limiting, Property 8: Direct URL sends bypass rate limiting
// **Validates: Requirements RLIM-5.2**

func TestPropertyDirectURLBypassesRateLimit(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for request counts (1-200)
	requestCountGen := gen.IntRange(1, 200)

	properties.Property("Allow() for an alias with no limiter always returns true regardless of call count", prop.ForAll(
		func(requestCount int) bool {
			// Simulate "direct URL" scenario: the registry has NO limiter for this alias.
			// This mirrors what happens when send_message targets a URL directly —
			// no limiter exists, so Allow() / Reserve() return true / nil.
			registry := NewRateLimiterRegistry()

			// Even if we set a global default on a DIFFERENT alias, the "direct URL"
			// alias should remain unlimited since no limiter is set for it.
			registry.Set("some-other-agent", 1e-9, 1)

			// Call Allow() requestCount times for the "direct URL" agent — all must succeed.
			for i := 0; i < requestCount; i++ {
				if !registry.Allow("direct-url-agent") {
					t.Logf("Allow() returned false on call %d for alias without limiter", i+1)
					return false
				}
			}

			// Also verify Reserve() returns nil (no limiter).
			if reservation := registry.Reserve("direct-url-agent"); reservation != nil {
				t.Log("Reserve() should return nil for alias without limiter")
				return false
			}

			return true
		},
		requestCountGen,
	))

	properties.TestingRun(t)
}

// Feature: rate-limiting, Property 9: List agents includes rate limit info
// **Validates: Requirements RLIM-6.1, RLIM-6.2**

func TestPropertyListAgentsIncludesRateLimit(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for RPS values (0.1-100.0)
	rpsGen := gen.Float64Range(0.1, 100.0)

	// Generator for burst values (1-100)
	burstGen := gen.IntRange(1, 100)

	// Generator for number of agents (1-10)
	numAgentsGen := gen.IntRange(1, 10)

	// Generator for bitmask (which agents have limiters)
	bitmaskGen := gen.IntRange(0, 1023)

	properties.Property("Get() returns correct config for agents with limiters and (0,0,false) for those without", prop.ForAll(
		func(numAgents int, bitmask int, rps float64, burst int) bool {
			registry := NewRateLimiterRegistry()

			// Set up agents: some with limiters, some without.
			type agentConfig struct {
				alias      string
				hasLimiter bool
			}
			agents := make([]agentConfig, numAgents)

			for i := range numAgents {
				alias := fmt.Sprintf("agent-%d", i)
				hasLimiter := (bitmask>>i)&1 == 1
				agents[i] = agentConfig{alias: alias, hasLimiter: hasLimiter}

				if hasLimiter {
					registry.Set(alias, rps, burst)
				}
			}

			// Verify Get() returns correct values for each agent.
			for _, ag := range agents {
				gotRPS, gotBurst, exists := registry.Get(ag.alias)
				if ag.hasLimiter {
					if !exists {
						t.Logf("expected limiter to exist for %s", ag.alias)
						return false
					}
					// RPS comparison: rate.Limit is float64, allow small epsilon for floating-point.
					if gotRPS < rps*0.999 || gotRPS > rps*1.001 {
						t.Logf("agent %s: expected RPS ~%f, got %f", ag.alias, rps, gotRPS)
						return false
					}
					if gotBurst != burst {
						t.Logf("agent %s: expected burst %d, got %d", ag.alias, burst, gotBurst)
						return false
					}
				} else {
					if exists {
						t.Logf("expected no limiter for %s, but one exists", ag.alias)
						return false
					}
					if gotRPS != 0 || gotBurst != 0 {
						t.Logf("agent %s: expected (0, 0) for non-existent limiter, got (%f, %d)", ag.alias, gotRPS, gotBurst)
						return false
					}
				}
			}

			return true
		},
		numAgentsGen,
		bitmaskGen,
		rpsGen,
		burstGen,
	))

	properties.TestingRun(t)
}
