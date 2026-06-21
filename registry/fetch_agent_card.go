package registry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// FetchAgentCard attempts to fetch an AgentCard from <agentURL>/.well-known/agent-card.json.
// Returns nil if the fetch fails for any reason (network error, non-200, invalid JSON).
func FetchAgentCard(ctx context.Context, httpClient *http.Client, agentURL string, headers map[string]string) *a2a.AgentCard {
	cardURL := strings.TrimRight(agentURL, "/") + "/.well-known/agent-card.json"

	client := httpClient
	if len(headers) > 0 {
		entry := &RegisteredAgent{Headers: headers}
		client = HTTPClientForAgent(httpClient, entry)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cardURL, nil)
	if err != nil {
		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var card a2a.AgentCard
	if err := json.Unmarshal(body, &card); err != nil {
		return nil
	}

	return &card
}
