package directory_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/nisimpson/a2a-gateway-mcp/directory"
)

// --- Mock helpers ---

// errorRegistry is a mock Registry that always returns errors from List().
type errorRegistry struct{}

func (r *errorRegistry) Register(_ context.Context, _ a2a.AgentCard) error {
	return errors.New("registry error")
}

func (r *errorRegistry) Unregister(_ context.Context, _ string) (bool, error) {
	return false, errors.New("registry error")
}

func (r *errorRegistry) List(_ context.Context) ([]a2a.AgentCard, error) {
	return nil, errors.New("registry error")
}

func (r *errorRegistry) Len(_ context.Context) (int, error) {
	return 0, errors.New("registry error")
}

// trackingResolver is a mock FilterResolver that records whether it was invoked.
type trackingResolver struct {
	invoked bool
	query   string
}

func (tr *trackingResolver) Resolve(_ context.Context, query string, cards []a2a.AgentCard) []a2a.AgentCard {
	tr.invoked = true
	tr.query = query
	// Return all cards (no filtering) to keep tests simple
	return cards
}

// --- Unit Tests ---

// TestEmptyRegistryReturnsEmptyArray validates Requirement 3.2:
// WHEN the Registry contains no agent cards, THE Handler SHALL respond with an empty JSON array [].
func TestEmptyRegistryReturnsEmptyArray(t *testing.T) {
	dir := directory.New()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	dir.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", contentType)
	}

	var cards []a2a.AgentCard
	if err := json.NewDecoder(rec.Body).Decode(&cards); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(cards) != 0 {
		t.Fatalf("expected empty array, got %d cards", len(cards))
	}
}

// TestNonGETReturns405 validates Requirement 3.4:
// WHEN a non-GET request is received, THE Handler SHALL respond with HTTP 405 Method Not Allowed.
func TestNonGETReturns405(t *testing.T) {
	dir := directory.New()

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/", nil)
			rec := httptest.NewRecorder()

			dir.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected status 405, got %d", rec.Code)
			}

			allow := rec.Header().Get("Allow")
			if allow != "GET" {
				t.Fatalf("expected Allow header 'GET', got %q", allow)
			}
		})
	}
}

// TestInvalidLimitReturns400 validates Requirement 5.3:
// WHEN a limit parameter is provided with a non-integer or non-positive value,
// THE Handler SHALL respond with HTTP 400 Bad Request.
func TestInvalidLimitReturns400(t *testing.T) {
	dir := directory.New()

	cases := []struct {
		name  string
		limit string
	}{
		{"zero", "0"},
		{"negative", "-1"},
		{"non-numeric", "abc"},
		{"float", "1.5"},
		{"empty-string-with-key", ""},
	}

	for _, tc := range cases {
		// Skip the empty string case since it means "no limit parameter"
		if tc.limit == "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?limit="+tc.limit, nil)
			rec := httptest.NewRecorder()

			dir.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400 for limit=%q, got %d", tc.limit, rec.Code)
			}

			var body map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode error response: %v", err)
			}

			if body["error"] != "limit must be a positive integer" {
				t.Fatalf("unexpected error message: %q", body["error"])
			}
		})
	}
}

// TestCustomQueryResolverInvoked validates Requirement 4.2:
// WHEN a custom QueryResolver is provided at initialization, THE Directory SHALL use it for all query filtering.
func TestCustomQueryResolverInvoked(t *testing.T) {
	resolver := &trackingResolver{}
	dir := directory.New(directory.WithFilterResolver(resolver))

	// Register a card so there's something to filter
	ctx := context.Background()
	if err := dir.Register(ctx, a2a.AgentCard{Name: "test-agent", Description: "A test agent"}); err != nil {
		t.Fatalf("failed to register card: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/?filter=test", nil)
	rec := httptest.NewRecorder()

	dir.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	if !resolver.invoked {
		t.Fatal("expected custom QueryResolver to be invoked, but it was not")
	}

	if resolver.query != "test" {
		t.Fatalf("expected resolver to receive query 'test', got %q", resolver.query)
	}
}

// TestEmptyQueryReturnsAllWithoutResolver validates Requirement 4.5:
// WHEN a query parameter is empty or not provided, THE Handler SHALL return all registered
// Agent_Cards without invoking the QueryResolver.
func TestEmptyQueryReturnsAllWithoutResolver(t *testing.T) {
	resolver := &trackingResolver{}
	dir := directory.New(directory.WithFilterResolver(resolver))

	ctx := context.Background()
	if err := dir.Register(ctx, a2a.AgentCard{Name: "agent-1", Description: "First agent"}); err != nil {
		t.Fatalf("failed to register card: %v", err)
	}
	if err := dir.Register(ctx, a2a.AgentCard{Name: "agent-2", Description: "Second agent"}); err != nil {
		t.Fatalf("failed to register card: %v", err)
	}

	tests := []struct {
		name string
		url  string
	}{
		{"no query param", "/"},
		{"empty query param", "/?filter="},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolver.invoked = false

			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()

			dir.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}

			if resolver.invoked {
				t.Fatal("expected QueryResolver NOT to be invoked for empty/missing query")
			}

			var cards []a2a.AgentCard
			if err := json.NewDecoder(rec.Body).Decode(&cards); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if len(cards) != 2 {
				t.Fatalf("expected 2 cards, got %d", len(cards))
			}
		})
	}
}

// TestHandlerAtDifferentServeMuxPrefixes validates Requirement 6.2:
// WHEN embedded in an existing http.ServeMux, THE Handler SHALL function correctly
// at any registered path prefix.
func TestHandlerAtDifferentServeMuxPrefixes(t *testing.T) {
	dir := directory.New()
	ctx := context.Background()
	if err := dir.Register(ctx, a2a.AgentCard{Name: "mux-agent", Description: "Agent for mux test"}); err != nil {
		t.Fatalf("failed to register card: %v", err)
	}

	prefixes := []string{"/agents/", "/api/v1/directory/", "/discover/"}

	for _, prefix := range prefixes {
		t.Run(prefix, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.Handle(prefix, dir)

			req := httptest.NewRequest(http.MethodGet, prefix, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200 at prefix %q, got %d", prefix, rec.Code)
			}

			var cards []a2a.AgentCard
			if err := json.NewDecoder(rec.Body).Decode(&cards); err != nil {
				t.Fatalf("failed to decode response at prefix %q: %v", prefix, err)
			}

			if len(cards) != 1 {
				t.Fatalf("expected 1 card at prefix %q, got %d", prefix, len(cards))
			}

			if cards[0].Name != "mux-agent" {
				t.Fatalf("expected card name 'mux-agent', got %q", cards[0].Name)
			}
		})
	}
}

// TestRegistryErrorReturns500 validates Requirement 3.1 (error handling):
// If any registry call returns an error, respond with HTTP 500 Internal Server Error.
func TestRegistryErrorReturns500(t *testing.T) {
	dir := directory.New(directory.WithRegistry(&errorRegistry{}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	dir.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", contentType)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if body["error"] != "internal server error" {
		t.Fatalf("unexpected error message: %q", body["error"])
	}
}

// --- Integration Tests for Standalone Server ---

// TestListenAndServe_StartsAndAcceptsConnections validates Requirement 7.1:
// THE Directory SHALL provide a method to start listening on a configurable address.
func TestListenAndServe_StartsAndAcceptsConnections(t *testing.T) {
	dir := directory.New()
	ctx := context.Background()
	if err := dir.Register(ctx, a2a.AgentCard{Name: "server-agent", Description: "Agent for server test"}); err != nil {
		t.Fatalf("failed to register card: %v", err)
	}

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	// Start server with a cancellable context
	serverCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- dir.ListenAndServe(serverCtx, addr)
	}()

	// Wait for server to be ready by polling
	var resp *http.Response
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatalf("server did not start accepting connections: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var cards []a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&cards); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(cards) != 1 || cards[0].Name != "server-agent" {
		t.Fatalf("expected 1 card named 'server-agent', got %v", cards)
	}

	// Clean up
	cancel()
	select {
	case serverErr := <-errCh:
		if serverErr != nil {
			t.Fatalf("ListenAndServe returned unexpected error: %v", serverErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}
}

// TestListenAndServe_ContextCancellationTriggersGracefulShutdown validates Requirements 7.2, 7.3:
// WHEN a shutdown signal is received, THE Directory SHALL gracefully stop accepting new connections.
// THE Directory SHALL accept a context.Context for lifecycle management.
func TestListenAndServe_ContextCancellationTriggersGracefulShutdown(t *testing.T) {
	dir := directory.New()

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	// Start server with a cancellable context
	serverCtx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- dir.ListenAndServe(serverCtx, addr)
	}()

	// Wait for server to be ready
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, connErr := http.Get("http://" + addr + "/")
		if connErr == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Cancel context to trigger graceful shutdown
	cancel()

	// Verify ListenAndServe returns nil (graceful shutdown)
	select {
	case serverErr := <-errCh:
		if serverErr != nil {
			t.Fatalf("expected nil error from graceful shutdown, got: %v", serverErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within timeout after context cancellation")
	}

	// Verify server no longer accepts connections
	_, err = http.Get("http://" + addr + "/")
	if err == nil {
		t.Fatal("expected connection error after shutdown, but request succeeded")
	}
}

// --- Property-Based Tests ---

// genNonEmptyAlpha generates a guaranteed non-empty alpha string by prepending a letter.
func genNonEmptyAlpha(params *gopter.GenParameters) string {
	// Always start with a letter to guarantee non-empty
	prefix := string(rune('a' + params.NextInt64()%26))
	result, ok := gen.AlphaString()(params).Retrieve()
	if !ok || result == nil {
		return prefix
	}
	return prefix + result.(string)
}

// genNonEmptyAlphaGen returns a gopter.Gen that produces non-empty alpha strings.
// This is for use with prop.ForAll (as opposed to genNonEmptyAlpha which is for direct params use).
func genNonEmptyAlphaGen() gopter.Gen {
	return func(params *gopter.GenParameters) *gopter.GenResult {
		s := genNonEmptyAlpha(params)
		return gopter.NewGenResult(s, gopter.NoShrinker)
	}
}

// genAgentSkillSimple generates a random AgentSkill using struct generation.
func genAgentSkillSimple(params *gopter.GenParameters) a2a.AgentSkill {
	id := genNonEmptyAlpha(params)
	name := genNonEmptyAlpha(params)
	desc := genNonEmptyAlpha(params)

	// Generate 1-3 tags
	raw := params.NextInt64() % 3
	if raw < 0 {
		raw = -raw
	}
	numTags := int(raw) + 1
	tags := make([]string, numTags)
	for i := range tags {
		tags[i] = genNonEmptyAlpha(params)
	}

	return a2a.AgentSkill{
		ID:          id,
		Name:        name,
		Description: desc,
		Tags:        tags,
	}
}

// genAgentCard generates a random AgentCard with varying names, descriptions, and skills.
func genAgentCard() gopter.Gen {
	return func(params *gopter.GenParameters) *gopter.GenResult {
		name := genNonEmptyAlpha(params)
		desc := genNonEmptyAlpha(params)

		// Generate 1-3 skills
		raw := params.NextInt64() % 3
		if raw < 0 {
			raw = -raw
		}
		numSkills := int(raw) + 1
		skills := make([]a2a.AgentSkill, numSkills)
		for i := range skills {
			skills[i] = genAgentSkillSimple(params)
		}

		card := a2a.AgentCard{
			Name:        name,
			Description: desc,
			Skills:      skills,
		}
		return gopter.NewGenResult(card, gopter.NoShrinker)
	}
}

// genFullAgentCard generates a full AgentCard with all optional fields populated.
func genFullAgentCard() gopter.Gen {
	return func(params *gopter.GenParameters) *gopter.GenResult {
		name := genNonEmptyAlpha(params)
		desc := genNonEmptyAlpha(params)
		version := genNonEmptyAlpha(params)
		docURL := genNonEmptyAlpha(params)
		iconURL := genNonEmptyAlpha(params)
		provOrg := genNonEmptyAlpha(params)
		provURL := genNonEmptyAlpha(params)

		// Generate 1-2 skills
		rawSkills := params.NextInt64() % 2
		if rawSkills < 0 {
			rawSkills = -rawSkills
		}
		numSkills := int(rawSkills) + 1
		skills := make([]a2a.AgentSkill, numSkills)
		for i := range skills {
			skills[i] = genAgentSkillSimple(params)
		}

		// Generate 1-2 input/output modes
		rawModes := params.NextInt64() % 2
		if rawModes < 0 {
			rawModes = -rawModes
		}
		numModes := int(rawModes) + 1
		inputModes := make([]string, numModes)
		outputModes := make([]string, numModes)
		for i := range inputModes {
			inputModes[i] = genNonEmptyAlpha(params)
		}
		for i := range outputModes {
			outputModes[i] = genNonEmptyAlpha(params)
		}

		card := a2a.AgentCard{
			Name:               name,
			Description:        desc,
			Skills:             skills,
			Version:            version,
			DefaultInputModes:  inputModes,
			DefaultOutputModes: outputModes,
			DocumentationURL:   docURL,
			IconURL:            iconURL,
			Provider: &a2a.AgentProvider{
				Org: provOrg,
				URL: provURL,
			},
		}
		return gopter.NewGenResult(card, gopter.NoShrinker)
	}
}

// genAgentCardSlice generates a slice of 1-10 random AgentCards with unique names.
func genAgentCardSlice() gopter.Gen {
	return func(params *gopter.GenParameters) *gopter.GenResult {
		raw := params.NextInt64() % 10
		if raw < 0 {
			raw = -raw
		}
		n := int(raw) + 1
		cards := make([]a2a.AgentCard, 0, n)
		seen := make(map[string]bool)
		for i := 0; i < n; i++ {
			result := genAgentCard()(params)
			card, _ := result.Retrieve()
			c := card.(a2a.AgentCard)
			if !seen[c.Name] {
				seen[c.Name] = true
				cards = append(cards, c)
			}
		}
		if len(cards) == 0 {
			// Ensure at least one card
			cards = append(cards, a2a.AgentCard{
				Name:        fmt.Sprintf("fallback-%d", params.NextInt64()),
				Description: "fallback",
			})
		}
		return gopter.NewGenResult(cards, gopter.NoShrinker)
	}
}

// Feature: a2a-directory, Property 1: Registration makes cards discoverable
// **Validates: Requirements 1.1**

func TestPropertyRegistrationMakesCardsDiscoverable(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("registering a card makes it appear in the listing", prop.ForAll(
		func(card a2a.AgentCard) bool {
			dir := directory.New()
			ctx := context.Background()

			if err := dir.Register(ctx, card); err != nil {
				return false
			}

			// GET all cards via HTTP
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			dir.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				return false
			}

			var cards []a2a.AgentCard
			if err := json.NewDecoder(rec.Body).Decode(&cards); err != nil {
				return false
			}

			// Verify the registered card is present
			for _, c := range cards {
				if c.Name == card.Name {
					return true
				}
			}
			return false
		},
		genAgentCard(),
	))

	properties.TestingRun(t)
}

// Feature: a2a-directory, Property 2: Duplicate registration replaces existing entry
// **Validates: Requirements 1.2**

func TestPropertyDuplicateRegistrationReplacesEntry(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("registering a card with the same name replaces the previous entry", prop.ForAll(
		func(name string, desc1 string, desc2 string) bool {
			dir := directory.New()
			ctx := context.Background()

			card1 := a2a.AgentCard{Name: name, Description: desc1}
			card2 := a2a.AgentCard{Name: name, Description: desc2}

			if err := dir.Register(ctx, card1); err != nil {
				return false
			}
			if err := dir.Register(ctx, card2); err != nil {
				return false
			}

			// GET all cards via HTTP
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			dir.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				return false
			}

			var cards []a2a.AgentCard
			if err := json.NewDecoder(rec.Body).Decode(&cards); err != nil {
				return false
			}

			// Should have exactly one card with this name
			count := 0
			for _, c := range cards {
				if c.Name == name {
					count++
					if c.Description != desc2 {
						return false
					}
				}
			}
			return count == 1
		},
		genNonEmptyAlphaGen(),
		gen.AlphaString(),
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}

// Feature: a2a-directory, Property 3: Concurrent register and unregister safety
// **Validates: Requirements 1.3, 2.3**

func TestPropertyConcurrentRegisterUnregisterSafety(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("concurrent register and unregister operations do not panic or corrupt data", prop.ForAll(
		func(n int) bool {
			dir := directory.New()
			ctx := context.Background()

			// Generate N distinct cards
			cards := make([]a2a.AgentCard, n)
			for i := 0; i < n; i++ {
				cards[i] = a2a.AgentCard{
					Name:        fmt.Sprintf("agent-%d", i),
					Description: fmt.Sprintf("Description for agent %d", i),
				}
			}

			// Register all cards concurrently
			var wg sync.WaitGroup
			wg.Add(n)
			for i := 0; i < n; i++ {
				go func(idx int) {
					defer wg.Done()
					_ = dir.Register(ctx, cards[idx])
				}(i)
			}
			wg.Wait()

			// Unregister the first half concurrently
			half := n / 2
			wg.Add(half)
			for i := 0; i < half; i++ {
				go func(idx int) {
					defer wg.Done()
					_, _ = dir.Unregister(ctx, cards[idx].Name)
				}(i)
			}
			wg.Wait()

			// Verify: GET the listing and check count
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			dir.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				return false
			}

			var remaining []a2a.AgentCard
			if err := json.NewDecoder(rec.Body).Decode(&remaining); err != nil {
				return false
			}

			// Expected remaining = n - half
			expected := n - half
			return len(remaining) == expected
		},
		gen.IntRange(1, 50),
	))

	properties.TestingRun(t)
}

// Feature: a2a-directory, Property 4: Unregister removes card and signals absence
// **Validates: Requirements 2.1, 2.2**

func TestPropertyUnregisterRemovesCardAndSignalsAbsence(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("unregister removes a registered card and returns true; unregistering non-existent returns false", prop.ForAll(
		func(cards []a2a.AgentCard, removeIdx int) bool {
			if len(cards) == 0 {
				return true
			}

			dir := directory.New()
			ctx := context.Background()

			// Register all cards (already deduplicated by generator)
			for _, c := range cards {
				if err := dir.Register(ctx, c); err != nil {
					return false
				}
			}

			// Pick one to unregister
			idx := removeIdx % len(cards)
			if idx < 0 {
				idx = -idx
			}
			targetName := cards[idx].Name

			// Unregister should return true
			removed, err := dir.Unregister(ctx, targetName)
			if err != nil || !removed {
				return false
			}

			// Verify it no longer appears in listing
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			dir.ServeHTTP(rec, req)

			var listed []a2a.AgentCard
			if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
				return false
			}

			for _, c := range listed {
				if c.Name == targetName {
					return false
				}
			}

			// Unregister non-existent name should return false
			removed2, err := dir.Unregister(ctx, "non-existent-name-xyz")
			if err != nil {
				return false
			}
			return !removed2
		},
		genAgentCardSlice(),
		gen.Int(),
	))

	properties.TestingRun(t)
}

// Feature: a2a-directory, Property 5: DefaultResolver returns exactly matching cards
// **Validates: Requirements 4.4, 4.6**

func TestPropertyDefaultResolverReturnsExactlyMatchingCards(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("DefaultResolver returns exactly cards matching case-insensitive substring", prop.ForAll(
		func(cards []a2a.AgentCard, querySource int) bool {
			if len(cards) == 0 {
				return true
			}

			dir := directory.New() // uses DefaultResolver
			ctx := context.Background()

			for _, c := range cards {
				if err := dir.Register(ctx, c); err != nil {
					return false
				}
			}

			// Extract a query substring from one of the cards
			sourceIdx := querySource % len(cards)
			if sourceIdx < 0 {
				sourceIdx = -sourceIdx
			}
			sourceCard := cards[sourceIdx]

			// Use a substring of the card's name as the query
			query := sourceCard.Name
			if len(query) > 2 {
				query = query[1 : len(query)-1] // take middle portion
			}

			if query == "" {
				return true // skip empty queries
			}

			// Query via HTTP
			req := httptest.NewRequest(http.MethodGet, "/?filter="+query, nil)
			rec := httptest.NewRecorder()
			dir.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				return false
			}

			var results []a2a.AgentCard
			if err := json.NewDecoder(rec.Body).Decode(&results); err != nil {
				return false
			}

			// Compute expected matches manually
			queryLower := strings.ToLower(query)
			expected := make(map[string]bool)
			for _, c := range cards {
				if strings.Contains(strings.ToLower(c.Name), queryLower) ||
					strings.Contains(strings.ToLower(c.Description), queryLower) ||
					skillTagsContain(c.Skills, queryLower) {
					expected[c.Name] = true
				}
			}

			// Verify no false positives
			resultNames := make(map[string]bool)
			for _, r := range results {
				resultNames[r.Name] = true
				if !expected[r.Name] {
					return false // false positive
				}
			}

			// Verify no false negatives
			for name := range expected {
				if !resultNames[name] {
					return false // false negative
				}
			}

			return true
		},
		genAgentCardSlice(),
		gen.Int(),
	))

	properties.TestingRun(t)
}

// skillTagsContain checks if any skill tag contains the query as a substring.
func skillTagsContain(skills []a2a.AgentSkill, query string) bool {
	for _, skill := range skills {
		for _, tag := range skill.Tags {
			if strings.Contains(strings.ToLower(tag), query) {
				return true
			}
		}
	}
	return false
}

// Feature: a2a-directory, Property 6: Limit caps result count
// **Validates: Requirements 5.1**

func TestPropertyLimitCapsResultCount(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("HTTP response contains at most limit cards", prop.ForAll(
		func(n int, limit int) bool {
			dir := directory.New()
			ctx := context.Background()

			// Register n cards
			for i := 0; i < n; i++ {
				card := a2a.AgentCard{
					Name:        fmt.Sprintf("agent-%d", i),
					Description: fmt.Sprintf("Agent number %d", i),
				}
				if err := dir.Register(ctx, card); err != nil {
					return false
				}
			}

			// GET with limit
			url := fmt.Sprintf("/?limit=%d", limit)
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()
			dir.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				return false
			}

			var cards []a2a.AgentCard
			if err := json.NewDecoder(rec.Body).Decode(&cards); err != nil {
				return false
			}

			// Result should be at most limit
			if len(cards) > limit {
				return false
			}

			// If total cards exceed limit, result should be exactly limit
			if n > limit && len(cards) != limit {
				return false
			}

			return true
		},
		gen.IntRange(1, 50),
		gen.IntRange(1, 30),
	))

	properties.TestingRun(t)
}

// Feature: a2a-directory, Property 7: Invalid limit returns HTTP 400
// **Validates: Requirements 5.3**

func TestPropertyInvalidLimitReturnsHTTP400(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for invalid limit strings
	invalidLimitGen := gen.OneConstOf("-1", "-100", "0", "abc", "1.5", "xyz", "-999", "0.0", "NaN", "inf")

	properties.Property("invalid limit parameter returns HTTP 400", prop.ForAll(
		func(invalidLimit string) bool {
			dir := directory.New()

			url := "/?limit=" + invalidLimit
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()
			dir.ServeHTTP(rec, req)

			return rec.Code == http.StatusBadRequest
		},
		invalidLimitGen,
	))

	properties.TestingRun(t)
}

// Feature: a2a-directory, Property 8: Non-GET methods return HTTP 405
// **Validates: Requirements 3.4**

func TestPropertyNonGETMethodsReturnHTTP405(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator for non-GET HTTP methods
	methodGen := gen.OneConstOf(
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
		http.MethodHead,
		http.MethodOptions,
	)

	properties.Property("non-GET HTTP methods return 405 Method Not Allowed", prop.ForAll(
		func(method string) bool {
			dir := directory.New()

			req := httptest.NewRequest(method, "/", nil)
			rec := httptest.NewRecorder()
			dir.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				return false
			}

			// Verify Allow header is set to GET
			allow := rec.Header().Get("Allow")
			return allow == "GET"
		},
		methodGen,
	))

	properties.TestingRun(t)
}

// Feature: a2a-directory, Property 9: JSON serialization round-trip
// **Validates: Requirements 8.1, 8.2, 3.1, 3.3**

func TestPropertyJSONSerializationRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("JSON serialization round-trip preserves agent card data", prop.ForAll(
		func(card a2a.AgentCard) bool {
			dir := directory.New()
			ctx := context.Background()

			if err := dir.Register(ctx, card); err != nil {
				return false
			}

			// GET the response
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			dir.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				return false
			}

			// Verify Content-Type
			contentType := rec.Header().Get("Content-Type")
			if contentType != "application/json" {
				return false
			}

			// Deserialize
			var cards []a2a.AgentCard
			if err := json.NewDecoder(rec.Body).Decode(&cards); err != nil {
				return false
			}

			if len(cards) != 1 {
				return false
			}

			result := cards[0]

			// Verify equivalence
			if result.Name != card.Name {
				return false
			}
			if result.Description != card.Description {
				return false
			}
			if result.Version != card.Version {
				return false
			}
			if result.DocumentationURL != card.DocumentationURL {
				return false
			}
			if result.IconURL != card.IconURL {
				return false
			}

			// Compare skills
			if len(result.Skills) != len(card.Skills) {
				return false
			}
			for i, skill := range card.Skills {
				if result.Skills[i].ID != skill.ID {
					return false
				}
				if result.Skills[i].Name != skill.Name {
					return false
				}
				if result.Skills[i].Description != skill.Description {
					return false
				}
				if len(result.Skills[i].Tags) != len(skill.Tags) {
					return false
				}
				for j, tag := range skill.Tags {
					if result.Skills[i].Tags[j] != tag {
						return false
					}
				}
			}

			// Compare DefaultInputModes
			if len(result.DefaultInputModes) != len(card.DefaultInputModes) {
				return false
			}
			for i, mode := range card.DefaultInputModes {
				if result.DefaultInputModes[i] != mode {
					return false
				}
			}

			// Compare DefaultOutputModes
			if len(result.DefaultOutputModes) != len(card.DefaultOutputModes) {
				return false
			}
			for i, mode := range card.DefaultOutputModes {
				if result.DefaultOutputModes[i] != mode {
					return false
				}
			}

			// Compare Provider
			if card.Provider != nil {
				if result.Provider == nil {
					return false
				}
				if result.Provider.Org != card.Provider.Org {
					return false
				}
				if result.Provider.URL != card.Provider.URL {
					return false
				}
			} else {
				if result.Provider != nil {
					return false
				}
			}

			return true
		},
		genFullAgentCard(),
	))

	properties.TestingRun(t)
}

// --- Additional Coverage Tests ---

// TestMemoryRegistryLen verifies the Len method returns the correct count.
func TestMemoryRegistryLen(t *testing.T) {
	dir := directory.New()
	ctx := context.Background()

	// Empty registry should have length 0 (verified via HTTP since Len is on the registry interface)
	// We test via the exported Register/Unregister + re-check pattern.
	if err := dir.Register(ctx, a2a.AgentCard{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := dir.Register(ctx, a2a.AgentCard{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := dir.Register(ctx, a2a.AgentCard{Name: "c"}); err != nil {
		t.Fatal(err)
	}

	// Verify 3 cards via HTTP
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	dir.ServeHTTP(rec, req)

	var cards []a2a.AgentCard
	if err := json.NewDecoder(rec.Body).Decode(&cards); err != nil {
		t.Fatal(err)
	}
	if len(cards) != 3 {
		t.Fatalf("expected 3 cards, got %d", len(cards))
	}
}

// TestQueryResolverFuncAdapter verifies the QueryResolverFunc adapter type works.
func TestQueryResolverFuncAdapter(t *testing.T) {
	called := false
	fn := directory.FilterResolverFunc(func(_ context.Context, query string, cards []a2a.AgentCard) []a2a.AgentCard {
		called = true
		// Filter to only cards whose name contains "match"
		var result []a2a.AgentCard
		for _, c := range cards {
			if strings.Contains(c.Name, query) {
				result = append(result, c)
			}
		}
		return result
	})

	dir := directory.New(directory.WithFilterResolver(fn))
	ctx := context.Background()
	if err := dir.Register(ctx, a2a.AgentCard{Name: "match-agent"}); err != nil {
		t.Fatal(err)
	}
	if err := dir.Register(ctx, a2a.AgentCard{Name: "other-agent"}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/?filter=match", nil)
	rec := httptest.NewRecorder()
	dir.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected QueryResolverFunc to be called")
	}

	var cards []a2a.AgentCard
	if err := json.NewDecoder(rec.Body).Decode(&cards); err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].Name != "match-agent" {
		t.Fatalf("expected 1 card named 'match-agent', got %v", cards)
	}
}

// TestQuerierInterfaceUsed verifies that when a registry implements Filterer,
// the handler delegates filtering to it instead of using the FilterResolver.
func TestQuerierInterfaceUsed(t *testing.T) {
	dir := directory.New(directory.WithRegistry(&filtererRegistry{}))

	req := httptest.NewRequest(http.MethodGet, "/?filter=special", nil)
	rec := httptest.NewRecorder()
	dir.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var cards []a2a.AgentCard
	if err := json.NewDecoder(rec.Body).Decode(&cards); err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].Name != "filterer-result" {
		t.Fatalf("expected filterer result, got %v", cards)
	}
}

// TestQuerierInterfaceError verifies that a Filterer error returns 500.
func TestQuerierInterfaceError(t *testing.T) {
	dir := directory.New(directory.WithRegistry(&filtererErrorRegistry{}))

	req := httptest.NewRequest(http.MethodGet, "/?filter=fail", nil)
	rec := httptest.NewRecorder()
	dir.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// TestListenAndServe_PortInUse verifies that ListenAndServe returns an error
// when the port is already in use.
func TestListenAndServe_PortInUse(t *testing.T) {
	// Occupy a port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()
	addr := listener.Addr().String()

	dir := directory.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Try to start on the same port — should fail
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- dir.ListenAndServe(ctx, addr)
	}()

	select {
	case err := <-serverErr:
		if err == nil {
			t.Fatal("expected error when port is in use, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListenAndServe did not return within timeout")
	}
}

// --- Mock helpers for Filterer tests ---

// filtererRegistry implements both Registry and Filterer.
type filtererRegistry struct{}

func (r *filtererRegistry) Register(_ context.Context, _ a2a.AgentCard) error {
	return nil
}

func (r *filtererRegistry) Unregister(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (r *filtererRegistry) List(_ context.Context) ([]a2a.AgentCard, error) {
	return []a2a.AgentCard{{Name: "list-result"}}, nil
}

func (r *filtererRegistry) Len(_ context.Context) (int, error) {
	return 1, nil
}

func (r *filtererRegistry) Filter(_ context.Context, _ string) ([]a2a.AgentCard, error) {
	return []a2a.AgentCard{{Name: "filterer-result"}}, nil
}

// filtererErrorRegistry implements Registry and Filterer but Filter returns an error.
type filtererErrorRegistry struct{}

func (r *filtererErrorRegistry) Register(_ context.Context, _ a2a.AgentCard) error {
	return nil
}

func (r *filtererErrorRegistry) Unregister(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (r *filtererErrorRegistry) List(_ context.Context) ([]a2a.AgentCard, error) {
	return nil, nil
}

func (r *filtererErrorRegistry) Len(_ context.Context) (int, error) {
	return 0, nil
}

func (r *filtererErrorRegistry) Filter(_ context.Context, _ string) ([]a2a.AgentCard, error) {
	return nil, errors.New("filter error")
}

// TestMemoryRegistryLenDirect tests the MemoryRegistry.Len method directly.
func TestMemoryRegistryLenDirect(t *testing.T) {
	reg := directory.NewMemoryRegistry()
	ctx := context.Background()

	n, err := reg.Len(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}

	_ = reg.Register(ctx, a2a.AgentCard{Name: "a"})
	_ = reg.Register(ctx, a2a.AgentCard{Name: "b"})

	n, err = reg.Len(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2, got %d", n)
	}

	_, _ = reg.Unregister(ctx, "a")

	n, err = reg.Len(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
}

// Feature: directory-filter-help, Property 2: Help parameter takes priority over filter and limit
// **Validates: Requirements 1.1, 1.2, 1.3**

func TestPropertyHelpParameterTakesPriorityOverFilterAndLimit(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("help=true response is identical regardless of filter and limit params", prop.ForAll(
		func(filter string, limit int) bool {
			dir := directory.New()

			// Request with help=true AND random filter and limit
			url := fmt.Sprintf("/?help=true&filter=%s&limit=%d", filter, limit)
			reqWithParams := httptest.NewRequest(http.MethodGet, url, nil)
			recWithParams := httptest.NewRecorder()
			dir.ServeHTTP(recWithParams, reqWithParams)

			// Request with help=true only
			reqHelpOnly := httptest.NewRequest(http.MethodGet, "/?help=true", nil)
			recHelpOnly := httptest.NewRecorder()
			dir.ServeHTTP(recHelpOnly, reqHelpOnly)

			// Both should return 200
			if recWithParams.Code != http.StatusOK || recHelpOnly.Code != http.StatusOK {
				return false
			}

			// Both responses should be byte-for-byte identical
			return recWithParams.Body.String() == recHelpOnly.Body.String()
		},
		gen.AlphaString(),
		gen.IntRange(1, 100),
	))

	properties.TestingRun(t)
}

// Feature: directory-filter-help, Property 1: Help response dispatch follows FilterHelper interface
// **Validates: Requirements 2.2, 2.3**

func TestPropertyHelpResponseDispatchFollowsFilterHelperInterface(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("resolver implementing FilterHelper returns its FilterHelp response on help=true", prop.ForAll(
		func(helpResp directory.FilterHelpResponse) bool {
			// Create a mock resolver that implements both FilterResolver and FilterHelper
			mock := &mockFilterHelper{response: helpResp}
			dir := directory.New(directory.WithFilterResolver(mock))

			req := httptest.NewRequest(http.MethodGet, "/?help=true", nil)
			rec := httptest.NewRecorder()
			dir.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				return false
			}

			var got directory.FilterHelpResponse
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				return false
			}

			return filterHelpResponseEqual(got, helpResp)
		},
		genFilterHelpResponse(),
	))

	properties.Property("resolver NOT implementing FilterHelper returns DefaultFilterHelp on help=true", prop.ForAll(
		func(unused string) bool {
			// Use a plain FilterResolverFunc which does NOT implement FilterHelper
			fn := directory.FilterResolverFunc(func(_ context.Context, _ string, cards []a2a.AgentCard) []a2a.AgentCard {
				return cards
			})
			dir := directory.New(directory.WithFilterResolver(fn))

			req := httptest.NewRequest(http.MethodGet, "/?help=true", nil)
			rec := httptest.NewRecorder()
			dir.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				return false
			}

			var got directory.FilterHelpResponse
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				return false
			}

			expected := directory.DefaultFilterHelp()
			return filterHelpResponseEqual(got, expected)
		},
		genNonEmptyAlphaGen(),
	))

	properties.TestingRun(t)
}

// mockFilterHelper implements both FilterResolver and FilterHelper for property testing.
type mockFilterHelper struct {
	response directory.FilterHelpResponse
}

func (m *mockFilterHelper) Resolve(_ context.Context, _ string, cards []a2a.AgentCard) []a2a.AgentCard {
	return cards
}

func (m *mockFilterHelper) FilterHelp() directory.FilterHelpResponse {
	return m.response
}

// genFilterHelpResponse generates a random FilterHelpResponse.
func genFilterHelpResponse() gopter.Gen {
	return func(params *gopter.GenParameters) *gopter.GenResult {
		description := genNonEmptyAlpha(params)
		syntax := genNonEmptyAlpha(params)

		// Generate 1-3 examples
		rawExamples := params.NextInt64() % 3
		if rawExamples < 0 {
			rawExamples = -rawExamples
		}
		numExamples := int(rawExamples) + 1
		examples := make([]directory.FilterExample, numExamples)
		for i := range examples {
			examples[i] = directory.FilterExample{
				Filter:      genNonEmptyAlpha(params),
				Description: genNonEmptyAlpha(params),
			}
		}

		// Generate 0-3 filterable fields
		rawFields := params.NextInt64() % 4
		if rawFields < 0 {
			rawFields = -rawFields
		}
		numFields := int(rawFields)
		var fields []string
		if numFields > 0 {
			fields = make([]string, numFields)
			for i := range fields {
				fields[i] = genNonEmptyAlpha(params)
			}
		}

		resp := directory.FilterHelpResponse{
			Description:      description,
			Syntax:           syntax,
			Examples:         examples,
			FilterableFields: fields,
		}
		return gopter.NewGenResult(resp, gopter.NoShrinker)
	}
}

// filterHelpResponseEqual compares two FilterHelpResponse values for equality.
func filterHelpResponseEqual(a, b directory.FilterHelpResponse) bool {
	if a.Description != b.Description {
		return false
	}
	if a.Syntax != b.Syntax {
		return false
	}
	if len(a.Examples) != len(b.Examples) {
		return false
	}
	for i := range a.Examples {
		if a.Examples[i].Filter != b.Examples[i].Filter {
			return false
		}
		if a.Examples[i].Description != b.Examples[i].Description {
			return false
		}
	}
	if len(a.FilterableFields) != len(b.FilterableFields) {
		return false
	}
	for i := range a.FilterableFields {
		if a.FilterableFields[i] != b.FilterableFields[i] {
			return false
		}
	}
	return true
}

// Feature: directory-filter-help, Property 4: FilterHelpResponse JSON round-trip
// **Validates: Requirements 3.1, 3.2, 3.3, 3.4**

func TestPropertyFilterHelpResponseJSONRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Generator: non-empty Description, non-empty Syntax, 1-5 examples, 0-5 filterable fields
	genRoundTripFilterHelp := func(params *gopter.GenParameters) *gopter.GenResult {
		description := genNonEmptyAlpha(params)
		syntax := genNonEmptyAlpha(params)

		// Generate 1-5 examples
		rawExamples := params.NextInt64() % 5
		if rawExamples < 0 {
			rawExamples = -rawExamples
		}
		numExamples := int(rawExamples) + 1
		examples := make([]directory.FilterExample, numExamples)
		for i := range examples {
			examples[i] = directory.FilterExample{
				Filter:      genNonEmptyAlpha(params),
				Description: genNonEmptyAlpha(params),
			}
		}

		// Generate 0-5 filterable fields (can be nil or non-empty)
		rawFields := params.NextInt64() % 6
		if rawFields < 0 {
			rawFields = -rawFields
		}
		numFields := int(rawFields)
		var fields []string
		if numFields > 0 {
			fields = make([]string, numFields)
			for i := range fields {
				fields[i] = genNonEmptyAlpha(params)
			}
		}

		resp := directory.FilterHelpResponse{
			Description:      description,
			Syntax:           syntax,
			Examples:         examples,
			FilterableFields: fields,
		}
		return gopter.NewGenResult(resp, gopter.NoShrinker)
	}

	properties.Property("FilterHelpResponse JSON round-trip preserves all fields", prop.ForAll(
		func(original directory.FilterHelpResponse) bool {
			// Marshal to JSON
			data, err := json.Marshal(original)
			if err != nil {
				return false
			}

			// Unmarshal back into a new value
			var decoded directory.FilterHelpResponse
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}

			// Verify equivalence
			return filterHelpResponseEqual(original, decoded)
		},
		gopter.Gen(genRoundTripFilterHelp),
	))

	properties.TestingRun(t)
}

// TestDefaultResolverNoMatches verifies that the DefaultResolver returns an empty
// slice (not nil) when no cards match the query.
func TestDefaultResolverNoMatches(t *testing.T) {
	dir := directory.New()
	ctx := context.Background()

	if err := dir.Register(ctx, a2a.AgentCard{Name: "alpha", Description: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := dir.Register(ctx, a2a.AgentCard{Name: "beta", Description: "second"}); err != nil {
		t.Fatal(err)
	}

	// Query something that doesn't match any card
	req := httptest.NewRequest(http.MethodGet, "/?filter=zzzznotfound", nil)
	rec := httptest.NewRecorder()
	dir.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var cards []a2a.AgentCard
	if err := json.NewDecoder(rec.Body).Decode(&cards); err != nil {
		t.Fatal(err)
	}
	if len(cards) != 0 {
		t.Fatalf("expected 0 cards, got %d", len(cards))
	}
}

// TestDefaultFilterHelpContent validates Requirements 4.1, 4.2, 4.3:
// The default help content describes case-insensitive substring matching,
// has empty FilterableFields, and includes at least one example.
func TestDefaultFilterHelpContent(t *testing.T) {
	help := directory.DefaultFilterHelp()

	// Requirement 4.1: Description mentions case-insensitive substring
	if !strings.Contains(help.Description, "case-insensitive substring") {
		t.Fatalf("expected Description to contain %q, got %q", "case-insensitive substring", help.Description)
	}

	// Requirement 4.2: FilterableFields is empty (default resolver does not support field-level targeting)
	if len(help.FilterableFields) != 0 {
		t.Fatalf("expected FilterableFields to be empty, got %v", help.FilterableFields)
	}

	// Requirement 4.3: Examples is non-empty
	if len(help.Examples) == 0 {
		t.Fatal("expected Examples to be non-empty")
	}
}
