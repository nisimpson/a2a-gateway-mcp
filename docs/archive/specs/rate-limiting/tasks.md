# Implementation Plan: Rate Limiting

## Overview

Implement per-agent rate limiting for the A2A Gateway MCP server using Go's `golang.org/x/time/rate` package. Each registered agent gets its own token bucket rate limiter, configurable via a server-wide default or per-agent override at connect time. Rate limiting is enforced before any outbound request, providing backpressure to MCP clients.

## Tasks

- [x] 1. Add `golang.org/x/time` dependency and create core rate limiter types
  - [x] 1.1 Add `golang.org/x/time` to go.mod
    - Run `go get golang.org/x/time`
    - This provides the `rate.Limiter` implementation (token bucket)
    - _Requirements: RLIM-1.5_

  - [x] 1.2 Create `gateway/rate_limiter.go` with `RateLimitConfig` and `RateLimiterRegistry`
    - Define `RateLimitConfig` struct with `RequestsPerSecond float64` and `Burst int` fields
    - Add `IsDisabled()` method returning true when RPS <= 0 or Burst <= 0
    - Define `RateLimiterRegistry` struct with `sync.RWMutex` and `map[string]*rate.Limiter`
    - Implement `NewRateLimiterRegistry()` constructor
    - Implement `Set(alias string, rps float64, burst int)` — creates/replaces limiter; removes entry if rps <= 0 or burst <= 0
    - Implement `Remove(alias string)` — deletes limiter for alias
    - Implement `Allow(alias string) bool` — returns true if token consumed or no limiter exists
    - Implement `Reserve(alias string) *rate.Reservation` — returns reservation or nil if no limiter
    - Implement `Get(alias string) (rps float64, burst int, exists bool)` — returns config for observability
    - Implement `Len() int` — returns count of active limiters
    - _Requirements: RLIM-1.1, RLIM-1.5, RLIM-1.7_

  - [x] 1.3 Implement `checkRateLimit` method on `Server`
    - Define `func (s *Server) checkRateLimit(alias string) *mcp.CallToolResult`
    - Use `Reserve()` to get reservation; if nil, return nil (unlimited)
    - If `reservation.Delay() == 0`, return nil (token consumed, proceed)
    - Otherwise, cancel reservation, compute wait time, return `IsError: true` with message: `rate limited: agent "<alias>" has exceeded its rate limit; retry after <duration>`
    - _Requirements: RLIM-2.1, RLIM-2.2_

- [x] 2. Extend server configuration with global default rate limit
  - [x] 2.1 Add rate limit fields to `serverConfig` and `Server` structs in `gateway/server.go`
    - Add `rateLimitRPS float64` and `rateLimitBurst int` to `serverConfig`
    - Add `rateLimiters *RateLimiterRegistry` and `defaultRateLimit *RateLimitConfig` fields to `Server`
    - _Requirements: RLIM-3.1_

  - [x] 2.2 Implement `WithRateLimit(requestsPerSecond float64, burst int) Option` functional option
    - Store RPS and burst in `serverConfig`
    - _Requirements: RLIM-3.1_

  - [x] 2.3 Update `NewServer()` to initialize `RateLimiterRegistry` and store default config
    - Create `NewRateLimiterRegistry()` and assign to `s.rateLimiters`
    - If `cfg.rateLimitRPS > 0 && cfg.rateLimitBurst > 0`, set `s.defaultRateLimit`
    - If both are zero or either is zero, leave `s.defaultRateLimit` as nil (no rate limiting — backward compatible)
    - _Requirements: RLIM-3.2, RLIM-3.4_

- [x] 3. Extend connect_agent with per-agent rate limit parameters
  - [x] 3.1 Add `RateLimitRPS` and `RateLimitBurst` fields to `ConnectAgentInput` in `gateway/tools.go`
    - Add `RateLimitRPS *float64` with json tag `rate_limit_rps,omitempty` and jsonschema description
    - Add `RateLimitBurst *int` with json tag `rate_limit_burst,omitempty` and jsonschema description
    - _Requirements: RLIM-4.1_

  - [x] 3.2 Add rate limit validation and limiter creation in `handleConnectAgent` in `gateway/tool_connect.go`
    - Validate: if only one of RateLimitRPS/RateLimitBurst is provided, return error "rate_limit_rps and rate_limit_burst must both be provided together"
    - Validate: if RateLimitRPS is negative, return error "rate_limit_rps must be non-negative"
    - Validate: if RateLimitBurst is negative, return error "rate_limit_burst must be non-negative"
    - If both provided: use per-agent values. If RPS is 0, skip creating limiter (disabled)
    - If neither provided: use `s.defaultRateLimit` (if non-nil) to create limiter
    - Call `s.rateLimiters.Set(alias, rps, burst)` to create/replace the limiter
    - When reconnecting (alias already exists), the `Set` call naturally replaces the old limiter
    - _Requirements: RLIM-4.2, RLIM-4.3, RLIM-4.4, RLIM-4.5, RLIM-1.2, RLIM-3.3_

- [x] 4. Integrate rate limiter removal on disconnect
  - [x] 4.1 Add `s.rateLimiters.Remove(input.Alias)` call in `handleDisconnectAgent` in `gateway/tool_disconnect.go`
    - Add the call after the registry disconnect succeeds (after `s.registry.Disconnect`)
    - _Requirements: RLIM-1.3_

- [x] 5. Checkpoint - Ensure compilation passes
  - Ensure all code compiles cleanly with `go build ./...`, ask the user if questions arise.

- [x] 6. Enforce rate limiting in send_message handler
  - [x] 6.1 Add rate limit check in `handleSendMessage` in `gateway/tool_send.go`
    - After resolving agent alias (after the `ResolveAgent` call), check `resolved.IsAlias`
    - If alias-based, call `s.checkRateLimit(resolved.Alias)`
    - If result is non-nil, return the rate limit error immediately (before client resolution)
    - Direct URL sends skip this check entirely
    - _Requirements: RLIM-1.4, RLIM-1.6, RLIM-5.1, RLIM-5.2, RLIM-5.3_

- [x] 7. Enforce rate limiting in broadcast_message handler
  - [x] 7.1 Add rate limit check in `broadcastToAgent` in `gateway/tool_broadcast.go`
    - After registry lookup (after `s.registry.Lookup(alias)`), before client resolution
    - Call `s.rateLimiters.Allow(alias)` — if false, return `broadcastResult{Status: "error", Error: "rate limited: agent \"<alias>\" has exceeded its rate limit"}`
    - This allows per-agent independent evaluation: rate-limited agents get errors, others proceed normally
    - _Requirements: RLIM-2.3, RLIM-2.4_

- [x] 8. Add rate limit info to list_agents response
  - [x] 8.1 Extend `listAgentEntry` and `handleListAgents` in `gateway/tool_list.go`
    - Add `RateLimit string` field to `listAgentEntry` with json tag `rate_limit`
    - In `handleListAgents`, for each entry call `s.rateLimiters.Get(entry.Alias)`
    - If exists: format as `"%.2f rps, burst %d"` (e.g., "10.00 rps, burst 20")
    - If not exists: set to `"unlimited"`
    - _Requirements: RLIM-6.1, RLIM-6.2_

- [x] 9. Checkpoint - Ensure all code compiles and existing tests pass
  - Run `go build ./...` and `go test ./gateway/...` to verify no regressions, ask the user if questions arise.

- [x] 10. Write property-based tests for rate limiter
  - [x] 10.1 Write property test for rate limiter count invariant
    - **Property 1: Rate limiter count invariant**
    - **Validates: Requirements RLIM-1.1, RLIM-1.3**
    - Create `gateway/rate_limiter_test.go`
    - Use `gopter` to generate random sequences of Set/Remove operations
    - After each sequence, verify `rateLimiters.Len()` matches expected count

  - [x] 10.2 Write property test for token consumption gating
    - **Property 2: Token consumption correctly gates requests**
    - **Validates: Requirements RLIM-1.4, RLIM-1.6, RLIM-5.1**
    - Generate random burst capacities (1-100)
    - Send burst+1 `Allow()` calls in rapid succession (no refill time)
    - Verify exactly `burst` calls return true and 1 returns false

  - [x] 10.3 Write property test for rate limit error message content
    - **Property 3: Rate limit error message contains required information**
    - **Validates: Requirements RLIM-2.2**
    - Generate random alias strings and rate limit configs
    - Exhaust the limiter, then call `checkRateLimit`
    - Verify error message contains the alias string and a parseable wait duration

  - [x] 10.4 Write property test for broadcast partial success
    - **Property 4: Broadcast evaluates limits independently with partial success**
    - **Validates: Requirements RLIM-2.3, RLIM-2.4**
    - Generate random sets of agents (some rate-limited, some not)
    - Verify broadcast results contain success for non-limited and error for rate-limited agents

  - [x] 10.5 Write property test for no rate limit means unlimited
    - **Property 5: No rate limit configured means unlimited throughput**
    - **Validates: Requirements RLIM-3.2**
    - Create registry with no limiters set
    - Generate random request counts (1-200)
    - Verify all `Allow()` calls return true

  - [x] 10.6 Write property test for per-agent config overriding global
    - **Property 6: Per-agent config overrides global default**
    - **Validates: Requirements RLIM-4.2, RLIM-1.2, RLIM-3.3**
    - Generate random global and per-agent configs with different burst values
    - Connect agent with per-agent config
    - Verify burst behavior matches per-agent config, not global

  - [x] 10.7 Write property test for reconnection replacing limiter
    - **Property 7: Reconnection replaces rate limiter with new config**
    - **Validates: Requirements RLIM-4.4**
    - Generate random config pairs
    - Connect, exhaust limiter, reconnect with new config
    - Verify new burst capacity matches the updated config

  - [x] 10.8 Write property test for concurrent rate limit safety
    - **Property 10: Concurrent rate limit checks are safe**
    - **Validates: Requirements RLIM-1.7**
    - Generate random goroutine counts (2-50) and burst sizes
    - Run concurrent `Allow()` calls with race detector
    - Verify no panics and total allowed does not exceed burst

- [x] 11. Write unit tests for connect, send, broadcast, and list integration
  - [x] 11.1 Write unit tests for connect_agent rate limit handling
    - Test: connect with both RPS and burst → limiter created with correct values
    - Test: connect with only RPS → returns validation error
    - Test: connect with only burst → returns validation error
    - Test: connect with zero RPS → rate limiting disabled for that agent
    - Test: reconnect with different config → limiter replaced
    - _Requirements: RLIM-4.1, RLIM-4.2, RLIM-4.3, RLIM-4.4, RLIM-4.5_

  - [x] 11.2 Write unit tests for send_message rate limiting
    - Test: send when within limit → request proceeds to agent
    - Test: send when rate limited → returns error with alias and retry time
    - Test: send by direct URL → no rate limiting applied
    - Test: rate limit check occurs before client resolution (no HTTP call when limited)
    - _Requirements: RLIM-1.4, RLIM-2.1, RLIM-2.2, RLIM-5.1, RLIM-5.2, RLIM-5.3_

  - [x] 11.3 Write unit tests for broadcast_message rate limiting
    - Test: broadcast to mix of rate-limited and non-limited agents → partial success
    - Test: broadcast when all agents within limit → all succeed
    - Test: broadcast when all agents rate limited → all report errors
    - _Requirements: RLIM-2.3, RLIM-2.4_

  - [x] 11.4 Write unit tests for list_agents rate limit display
    - Test: list with rate-limited agents shows config values
    - Test: list with unlimited agents shows "unlimited"
    - Test: list with mix of both shows correct values for each
    - _Requirements: RLIM-6.1, RLIM-6.2_

- [x] 12. Final checkpoint - Ensure all tests pass with race detector
  - Run `go test -race ./gateway/...` to verify all tests pass with no data races, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- `golang.org/x/time` is not yet in go.mod and needs to be added explicitly
- The `gopter` library is already available in go.mod for property-based tests
- Checkpoints ensure incremental validation after core implementation and before tests
- Property tests validate universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
- The race detector (`-race` flag) validates concurrent safety (Property 10)
