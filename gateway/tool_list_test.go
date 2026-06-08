package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Feature: agent-health-checks, Property 6: List agents health reporting correctness
// **Validates: Requirements HLTH-2.1, HLTH-2.2, HLTH-2.3, HLTH-2.4**

func TestPropertyListAgentsHealthReporting(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for number of agents (1-10)
	agentCountGen := gen.IntRange(1, 10)

	// Generator for health state per agent: 0=healthy, 1=unhealthy, 2=unknown
	healthStateGen := gen.IntRange(0, 2)

	// Generator for failure threshold (1-10)
	thresholdGen := gen.IntRange(1, 10)

	properties.Property("list_agents includes health field for all agents, consecutive_failures only for unhealthy, all agents present", prop.ForAll(
		func(agentCount int, healthStateSeed int, threshold int) bool {
			// Create server with health tracking enabled.
			srv := NewServer(WithHealthCheck(HealthCheckOptions{
				FailureThreshold: threshold,
				PingStrategy:     &recordingPingStrategy{},
			}))

			// Generate agents and drive them to different health states.
			type agentExpected struct {
				alias          string
				expectedHealth HealthStatus
			}
			agents := make([]agentExpected, agentCount)

			for i := range agentCount {
				alias := "agent-" + string(rune('a'+i))
				url := "http://" + alias + ".example.com"

				srv.registry.Connect(alias, url, nil, "")
				srv.healthTracker.Reset(alias)

				// Determine health state using seed + index to vary across agents.
				state := (healthStateSeed + i) % 3

				switch state {
				case 0: // healthy
					srv.healthTracker.RecordSuccess(alias)
					agents[i] = agentExpected{alias: alias, expectedHealth: HealthStatusHealthy}
				case 1: // unhealthy
					for j := 0; j < threshold; j++ {
						srv.healthTracker.RecordFailure(alias)
					}
					agents[i] = agentExpected{alias: alias, expectedHealth: HealthStatusUnhealthy}
				case 2: // unknown (leave as initial state)
					agents[i] = agentExpected{alias: alias, expectedHealth: HealthStatusUnknown}
				}
			}

			// Call handleListAgents.
			result, _, err := srv.handleListAgents(context.Background(), nil, ListAgentsInput{})
			if err != nil {
				t.Logf("handleListAgents returned error: %v", err)
				return false
			}
			if result.IsError {
				t.Log("handleListAgents returned MCP error result")
				return false
			}

			// Parse the JSON response.
			text := extractText(t, result)
			var entries []listAgentEntry
			if err := json.Unmarshal([]byte(text), &entries); err != nil {
				t.Logf("failed to unmarshal list_agents response: %v", err)
				return false
			}

			// HLTH-2.4: All registered agents appear in the response.
			if len(entries) != agentCount {
				t.Logf("expected %d entries, got %d", agentCount, len(entries))
				return false
			}

			// Build a map of entries by alias for verification.
			entryMap := make(map[string]listAgentEntry, len(entries))
			for _, e := range entries {
				entryMap[e.Alias] = e
			}

			for _, expected := range agents {
				entry, exists := entryMap[expected.alias]
				if !exists {
					// HLTH-2.4: Agent must appear regardless of health status.
					t.Logf("agent %q missing from response", expected.alias)
					return false
				}

				// HLTH-2.1: Every agent has a health field with valid value.
				switch entry.Health {
				case "healthy", "unhealthy", "unknown":
					// valid
				default:
					t.Logf("invalid health value %q for agent %q", entry.Health, expected.alias)
					return false
				}

				// Verify health matches expected state.
				if entry.Health != string(expected.expectedHealth) {
					t.Logf("expected health=%q for agent %q, got %q", expected.expectedHealth, expected.alias, entry.Health)
					return false
				}

				// HLTH-2.2: consecutive_failures present ONLY for unhealthy agents.
				// HLTH-2.3: consecutive_failures omitted for healthy/unknown.
				if expected.expectedHealth == HealthStatusUnhealthy {
					if entry.ConsecutiveFailures == nil {
						t.Logf("expected consecutive_failures for unhealthy agent %q", expected.alias)
						return false
					}
					if *entry.ConsecutiveFailures < threshold {
						t.Logf("expected consecutive_failures >= %d for unhealthy agent %q, got %d", threshold, expected.alias, *entry.ConsecutiveFailures)
						return false
					}
				} else {
					if entry.ConsecutiveFailures != nil {
						t.Logf("expected no consecutive_failures for %s agent %q, got %d", expected.expectedHealth, expected.alias, *entry.ConsecutiveFailures)
						return false
					}
				}
			}

			return true
		},
		agentCountGen,
		healthStateGen,
		thresholdGen,
	))

	properties.TestingRun(t)
}
