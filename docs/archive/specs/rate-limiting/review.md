# Spec Review: Rate Limiting

## Summary

The rate-limiting implementation is well-executed and complete. All 22 acceptance criteria across 6 requirements are satisfied. The code follows the design document closely, uses appropriate patterns (functional options, separate registry, enforcement before client resolution), and has comprehensive test coverage including 10 property-based tests and 16 unit tests. All tests pass with the race detector enabled. All issues from the initial review have been resolved.

## Issues

| # | Severity | File | Requirement | Description | Fix |
|---|----------|------|-------------|-------------|-----|
| 1 | Low | `go.mod` | RLIM-1.5 | `golang.org/x/time` is listed as `// indirect` despite being directly imported by `gateway/rate_limiter.go`. This happens because `go get` was run before the import existed. | ~~Run `go mod tidy` to fix the indirect marker.~~ **FIXED** |
| 2 | Low | `gateway/rate_limiter_test.go` | RLIM-5.2 | Property 8 (Direct URL sends bypass rate limiting) from the design doc is not implemented as a property-based test. It's covered by the unit test `TestSendMessage_DirectURL_NoRateLimit` but lacks formal PBT validation. | ~~Optionally add a property test or accept unit test coverage.~~ **FIXED**: Added `TestPropertyDirectURLBypassesRateLimit` |
| 3 | Low | `gateway/rate_limiter_test.go` | RLIM-6.1, RLIM-6.2 | Property 9 (List agents includes rate limit info) from the design doc is not implemented as a property-based test. It's covered by unit tests in `tool_list_rate_limit_test.go`. | ~~Optionally add a property test or accept unit test coverage.~~ **FIXED**: Added `TestPropertyListAgentsIncludesRateLimit` |
| 4 | Trivial | `gateway/rate_limiter.go` | — | `RateLimitConfig.IsDisabled()` is defined but never called in production code. The check is done inline in `handleConnectAgent`. | ~~Remove or use consistently.~~ **FIXED**: Refactored to use `IsDisabled()` |

## Requirements Compliance

| Requirement | Criterion | Status | Evidence |
|-------------|-----------|--------|----------|
| RLIM-1.1 | One Rate_Limiter per Agent_Entry | ✅ Pass | `RateLimiterRegistry` maintains `map[string]*rate.Limiter`; PBT Property 1 validates count invariant |
| RLIM-1.2 | Create limiter on connect (per-agent or global) | ✅ Pass | `handleConnectAgent` lines 77-89 create limiter from per-agent or global default |
| RLIM-1.3 | Remove limiter on disconnect | ✅ Pass | `handleDisconnectAgent` calls `s.rateLimiters.Remove(input.Alias)` |
| RLIM-1.4 | Consume token before forwarding | ✅ Pass | `handleSendMessage` calls `checkRateLimit`; `broadcastToAgent` calls `Allow()` |
| RLIM-1.5 | Token bucket with configurable RPS/Burst | ✅ Pass | Uses `golang.org/x/time/rate` with `rate.NewLimiter(rate.Limit(rps), burst)` |
| RLIM-1.6 | Proceed when token consumed | ✅ Pass | `checkRateLimit` returns nil when `reservation.Delay() == 0`; handler continues |
| RLIM-1.7 | Thread-safe | ✅ Pass | `sync.RWMutex` protects registry; `rate.Limiter` is inherently thread-safe; PBT Property 10 validates with concurrent goroutines |
| RLIM-2.1 | Reject immediately without queuing | ✅ Pass | `checkRateLimit` cancels reservation and returns immediately; no queue |
| RLIM-2.2 | Error with alias, rate limited message, wait time | ✅ Pass | Error format: `rate limited: agent %q has exceeded its rate limit; retry after %s`; PBT Property 3 validates |
| RLIM-2.3 | Broadcast evaluates limits independently | ✅ Pass | `broadcastToAgent` checks `Allow(alias)` per-agent; other agents proceed |
| RLIM-2.4 | Broadcast returns mixed results | ✅ Pass | `TestBroadcast_MixedRateLimits` validates partial success; PBT Property 4 validates at registry level |
| RLIM-3.1 | `WithRateLimit` functional option | ✅ Pass | `WithRateLimit(requestsPerSecond float64, burst int) Option` implemented in `server.go` |
| RLIM-3.2 | No default = unlimited (backward compat) | ✅ Pass | `defaultRateLimit` is nil when not configured; `checkRateLimit` returns nil for missing limiter |
| RLIM-3.3 | Global default applies to agents without per-agent config | ✅ Pass | `handleConnectAgent` line 87: `s.rateLimiters.Set(input.Alias, s.defaultRateLimit.RequestsPerSecond, s.defaultRateLimit.Burst)` |
| RLIM-3.4 | Zero RPS/Burst in global = disabled | ✅ Pass | `NewServer` only sets `defaultRateLimit` when `cfg.rateLimitRPS > 0 && cfg.rateLimitBurst > 0` |
| RLIM-4.1 | Optional `rate_limit_rps` and `rate_limit_burst` fields | ✅ Pass | `ConnectAgentInput` has `RateLimitRPS *float64` and `RateLimitBurst *int` with correct JSON/jsonschema tags |
| RLIM-4.2 | Per-agent values override global | ✅ Pass | `handleConnectAgent` uses per-agent values when both provided; PBT Property 6 validates |
| RLIM-4.3 | Error when only one provided | ✅ Pass | `(input.RateLimitRPS != nil) != (input.RateLimitBurst != nil)` check returns error |
| RLIM-4.4 | Reconnect replaces limiter | ✅ Pass | `Set()` naturally replaces; `TestConnectAgent_Reconnect_ReplacesLimiter` validates; PBT Property 7 validates |
| RLIM-4.5 | Zero RPS disables for agent | ✅ Pass | When RPS is 0, `s.rateLimiters.Remove(input.Alias)` is called |
| RLIM-5.1 | Alias-based sends apply limiter | ✅ Pass | `if resolved.IsAlias { s.checkRateLimit(resolved.Alias) }` |
| RLIM-5.2 | Direct URL sends skip limiting | ✅ Pass | Rate limit check only runs when `resolved.IsAlias`; `TestSendMessage_DirectURL_NoRateLimit` validates |
| RLIM-5.3 | Check before client resolution | ✅ Pass | `checkRateLimit` call is before `s.clients.Resolve()`; `TestSendMessage_RateLimitBeforeClientResolution` validates |
| RLIM-6.1 | List shows rate limit config | ✅ Pass | `handleListAgents` includes `RateLimit` field formatted as `"%.2f rps, burst %d"` or `"unlimited"` |
| RLIM-6.2 | Rate limit values alongside alias/URL | ✅ Pass | `listAgentEntry` struct includes `Alias`, `URL`, and `RateLimit` fields |

## Property-Based Test Coverage

| Property | Test Name | Status | Notes |
|----------|-----------|--------|-------|
| 1: Rate limiter count invariant | `TestPropertyRateLimiterCountInvariant` | ✅ Pass | 100 iterations, random Set/Remove sequences |
| 2: Token consumption gates requests | `TestPropertyTokenConsumptionGatesRequests` | ✅ Pass | 100 iterations, random burst 1-100 |
| 3: Error message content | `TestPropertyRateLimitErrorMessage` | ✅ Pass | 100 iterations, random aliases and configs |
| 4: Broadcast partial success | `TestPropertyBroadcastPartialSuccess` | ✅ Pass | 100 iterations, random agent mixes |
| 5: No limit = unlimited | `TestPropertyNoRateLimitUnlimited` | ✅ Pass | 100 iterations, random request counts |
| 6: Per-agent overrides global | `TestPropertyPerAgentOverridesGlobal` | ✅ Pass | 100 iterations, distinct burst ranges |
| 7: Reconnection replaces limiter | `TestPropertyReconnectionReplacesLimiter` | ✅ Pass | 100 iterations, distinct config pairs |
| 8: Direct URL bypasses rate limit | `TestPropertyDirectURLBypassesRateLimit` | ✅ Pass | 100 iterations, random request counts 1-200 |
| 9: List agents includes rate limit | `TestPropertyListAgentsIncludesRateLimit` | ✅ Pass | 100 iterations, random agent sets with mixed configs |
| 10: Concurrent safety | `TestPropertyConcurrentRateLimitSafety` | ✅ Pass | 100 iterations, 2-50 goroutines, race detector |

## Design Decision Adherence

| Decision | Followed? | Notes |
|----------|-----------|-------|
| D1: Separate `RateLimiterRegistry` | ✅ Yes | `RateLimiterRegistry` in `rate_limiter.go`; `AgentRegistry` unchanged |
| D2: `golang.org/x/time/rate` | ✅ Yes | Uses `rate.NewLimiter`, `rate.Limit`, `*rate.Limiter` throughout |
| D3: Check before client resolution | ✅ Yes | In `handleSendMessage`: after `ResolveAgent`, before `s.clients.Resolve` |
| D4: Direct URL skips rate limiting | ✅ Yes | Only `resolved.IsAlias` triggers `checkRateLimit` |
| D5: `Reserve()` for send (with wait time) | ✅ Yes | `checkRateLimit` uses `Reserve()` + `Delay()` for the error message |
| D6: `Allow()` for broadcast (simpler) | ✅ Yes | `broadcastToAgent` uses `Allow()` for non-blocking check |

## Deviations

| # | Area | Design Doc | Actual | Rationale |
|---|------|-----------|--------|-----------|
| 1 | Test file organization | Design doc lists tests in `gateway/tool_connect_test.go`, `gateway/tool_send_test.go`, `gateway/tool_broadcast_test.go` | Separate files: `*_rate_limit_test.go` | Better organization; keeps rate limit tests isolated from existing test files |
| 2 | PBT library | Design doc references `gopter` for all 10 properties | Properties 8 and 9 use unit tests | These properties are integration-level (require HTTP mocking) and are less suitable for PBT generators; unit tests provide adequate coverage |

## Action Items

1. ~~**Low**: Run `go mod tidy` to fix `golang.org/x/time` indirect marker~~ — **RESOLVED**
2. ~~**Low/Optional**: Consider adding PBT for Properties 8 and 9 if stronger correctness guarantees are desired~~ — **RESOLVED**: Added `TestPropertyDirectURLBypassesRateLimit` and `TestPropertyListAgentsIncludesRateLimit`
3. ~~**Trivial/Optional**: Decide whether to use `RateLimitConfig.IsDisabled()` in `handleConnectAgent` instead of inline checks, or remove the method~~ — **RESOLVED**: Refactored `handleConnectAgent` to use `IsDisabled()` consistently

All issues resolved. The implementation is ready for merge.
