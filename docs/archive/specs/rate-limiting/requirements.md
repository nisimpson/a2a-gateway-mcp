# Requirements Document

## Traceability

**Prefix**: `RLIM`  
**Format**: `RLIM-{requirement}.{criterion}`  
**Feature**: rate-limiting

## Introduction

This feature adds per-agent rate limiting and backpressure to the A2A Gateway MCP server. Currently, an MCP client can fire unlimited `send_message` or `broadcast_message` calls without any throttling, potentially overwhelming target A2A agents. Rate limiting protects remote agents from being overloaded during rapid sends or large fan-out broadcasts, while providing clear feedback to callers when limits are reached.

## Glossary

- **Rate_Limiter**: An in-memory, per-agent component that tracks request frequency and enforces configured rate limits using a token bucket algorithm
- **Token_Bucket**: A rate-limiting algorithm where tokens are added at a fixed rate and consumed per request; requests are rejected when no tokens are available
- **Burst_Capacity**: The maximum number of tokens a bucket can hold, allowing short bursts of traffic above the sustained rate
- **Requests_Per_Second**: The sustained rate at which tokens are replenished in the bucket (the steady-state throughput limit)
- **Rate_Limit_Config**: A configuration specifying requests_per_second and burst_capacity for a rate limiter
- **Global_Default_Rate_Limit**: The server-wide Rate_Limit_Config applied to agents that do not have a per-agent override
- **Per_Agent_Rate_Limit**: A Rate_Limit_Config specified at agent connect time that overrides the Global_Default_Rate_Limit for that specific agent
- **Gateway_Server**: The A2A Gateway MCP server that bridges MCP clients to remote A2A agents
- **Agent_Entry**: A registered agent in the registry, identified by alias

## Requirements

### Requirement 1: Token Bucket Rate Limiter

**User Story:** As a gateway operator, I want each connected agent to have its own rate limiter, so that one agent's traffic does not affect another and no single agent is overwhelmed.

#### Acceptance Criteria

1. **[RLIM-1.1]** THE Gateway_Server SHALL maintain one Rate_Limiter instance per registered Agent_Entry
2. **[RLIM-1.2]** WHEN a new agent is connected via the `connect_agent` tool, THE Gateway_Server SHALL create a Rate_Limiter for that agent using either the Per_Agent_Rate_Limit (if provided) or the Global_Default_Rate_Limit
3. **[RLIM-1.3]** WHEN an agent is disconnected via the `disconnect_agent` tool, THE Gateway_Server SHALL remove the Rate_Limiter associated with that agent
4. **[RLIM-1.4]** WHEN a `send_message` or `broadcast_message` request targets an agent, THE Gateway_Server SHALL attempt to consume one token from that agent's Rate_Limiter before forwarding the request
5. **[RLIM-1.5]** THE Rate_Limiter SHALL use the Token_Bucket algorithm with configurable Requests_Per_Second and Burst_Capacity parameters
6. **[RLIM-1.6]** WHEN a token is successfully consumed, THE Gateway_Server SHALL proceed with forwarding the request to the target agent
7. **[RLIM-1.7]** THE Rate_Limiter SHALL be safe for concurrent access from multiple goroutines

### Requirement 2: Rate Limit Rejection Behavior

**User Story:** As an MCP client, I want clear feedback when my request is rate limited, so that I can implement retry logic or adjust my sending frequency.

#### Acceptance Criteria

1. **[RLIM-2.1]** WHEN a token cannot be consumed (bucket is empty), THE Gateway_Server SHALL reject the request immediately without queuing
2. **[RLIM-2.2]** WHEN a request is rejected due to rate limiting, THE Gateway_Server SHALL return an MCP tool result with `IsError: true` and a message indicating the agent alias, that the request was rate limited, and the estimated wait time before a token becomes available
3. **[RLIM-2.3]** WHEN a broadcast_message targets multiple agents, THE Gateway_Server SHALL evaluate rate limits independently per target agent; agents within their limit SHALL receive the message while rate-limited agents SHALL report an error in their individual broadcast result
4. **[RLIM-2.4]** WHEN a broadcast_message has some agents rate-limited and others not, THE Gateway_Server SHALL return a mixed result containing both successful responses and rate-limit errors (partial success)

### Requirement 3: Global Default Rate Limit Configuration

**User Story:** As a gateway operator, I want to set a server-wide default rate limit, so that all agents are protected without requiring per-agent configuration.

#### Acceptance Criteria

1. **[RLIM-3.1]** THE Gateway_Server SHALL accept a Global_Default_Rate_Limit via a `WithRateLimit(requestsPerSecond float64, burst int)` functional option at server initialization
2. **[RLIM-3.2]** WHEN no Global_Default_Rate_Limit is configured, THE Gateway_Server SHALL apply no rate limiting (unlimited throughput) to maintain backward compatibility
3. **[RLIM-3.3]** THE Global_Default_Rate_Limit SHALL apply to all agents that do not have a Per_Agent_Rate_Limit specified at connect time
4. **[RLIM-3.4]** WHEN the Global_Default_Rate_Limit specifies a Requests_Per_Second of zero or a Burst_Capacity of zero, THE Gateway_Server SHALL treat that agent as having no rate limit (disabled)

### Requirement 4: Per-Agent Rate Limit Override at Connect Time

**User Story:** As a gateway operator, I want to set different rate limits for different agents at connect time, so that I can give higher limits to agents that handle more traffic and lower limits to agents that are more sensitive.

#### Acceptance Criteria

1. **[RLIM-4.1]** THE `connect_agent` tool input schema SHALL accept optional `rate_limit_rps` (float, requests per second) and `rate_limit_burst` (integer, burst capacity) parameters
2. **[RLIM-4.2]** WHEN `rate_limit_rps` and `rate_limit_burst` are provided in a connect_agent request, THE Gateway_Server SHALL create the agent's Rate_Limiter using those values instead of the Global_Default_Rate_Limit
3. **[RLIM-4.3]** WHEN only one of `rate_limit_rps` or `rate_limit_burst` is provided, THE Gateway_Server SHALL return an MCP error response indicating both parameters must be provided together
4. **[RLIM-4.4]** WHEN an agent is reconnected (alias already exists), THE Gateway_Server SHALL replace the existing Rate_Limiter with a new one using the updated configuration
5. **[RLIM-4.5]** WHEN `rate_limit_rps` is set to zero in a connect_agent request, THE Gateway_Server SHALL disable rate limiting for that specific agent regardless of the Global_Default_Rate_Limit

### Requirement 5: Rate Limiting for Direct URL Sends

**User Story:** As an MCP client, I want rate limiting applied consistently whether I send messages by alias or by direct URL, so that protection is uniform.

#### Acceptance Criteria

1. **[RLIM-5.1]** WHEN a `send_message` request targets an agent by alias, THE Gateway_Server SHALL apply the Rate_Limiter associated with that alias
2. **[RLIM-5.2]** WHEN a `send_message` request targets an agent by direct URL (not an alias), THE Gateway_Server SHALL NOT apply rate limiting, since no Rate_Limiter is associated with unregistered URLs
3. **[RLIM-5.3]** THE Gateway_Server SHALL evaluate rate limits before resolving the SDK client or making any outbound HTTP request to the target agent

### Requirement 6: Rate Limiter State Observability

**User Story:** As an MCP client, I want to see rate limit configuration for connected agents, so that I can understand the constraints when planning message patterns.

#### Acceptance Criteria

1. **[RLIM-6.1]** WHEN the `list_agents` tool is invoked, THE Gateway_Server SHALL include each agent's rate limit configuration (requests_per_second and burst_capacity) in the output, or indicate "unlimited" if no rate limit is configured
2. **[RLIM-6.2]** WHEN an agent has a rate limit configured, THE Gateway_Server SHALL display the rate limit values in the list_agents response alongside the agent's alias and URL
