package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultPingStrategy_DefaultEndpoint(t *testing.T) {
	// Start a test server that responds on /.well-known/agent-card.json
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-card.json" {
			t.Errorf("expected path /.well-known/agent-card.json, got %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET method, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"test"}`))
	}))
	defer server.Close()

	strategy := NewDefaultPingStrategy(server.Client())
	target := PingTarget{
		Alias: "test-agent",
		URL:   server.URL,
	}

	result := strategy.Ping(context.Background(), target)
	if !result.Reachable {
		t.Fatalf("expected reachable, got unreachable: %v", result.Err)
	}
	if result.ResponseTime <= 0 {
		t.Errorf("expected positive response time, got %v", result.ResponseTime)
	}
	if result.Err != nil {
		t.Errorf("expected nil error, got %v", result.Err)
	}
}

func TestDefaultPingStrategy_CustomEndpoint(t *testing.T) {
	// Start a test server that responds on a custom path
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/check" {
			t.Errorf("expected path /health/check, got %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	strategy := NewDefaultPingStrategy(server.Client())
	target := PingTarget{
		Alias:        "test-agent",
		URL:          server.URL,
		PingEndpoint: "/health/check",
	}

	result := strategy.Ping(context.Background(), target)
	if !result.Reachable {
		t.Fatalf("expected reachable, got unreachable: %v", result.Err)
	}
}

func TestDefaultPingStrategy_AppliesHeaders(t *testing.T) {
	// Start a test server that verifies headers are present
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer my-token" {
			t.Errorf("expected Authorization header 'Bearer my-token', got %q", authHeader)
		}
		customHeader := r.Header.Get("X-Custom-Header")
		if customHeader != "custom-value" {
			t.Errorf("expected X-Custom-Header 'custom-value', got %q", customHeader)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	strategy := NewDefaultPingStrategy(server.Client())
	target := PingTarget{
		Alias: "test-agent",
		URL:   server.URL,
		Headers: map[string]string{
			"Authorization":   "Bearer my-token",
			"X-Custom-Header": "custom-value",
		},
	}

	result := strategy.Ping(context.Background(), target)
	if !result.Reachable {
		t.Fatalf("expected reachable, got unreachable: %v", result.Err)
	}
}

func TestDefaultPingStrategy_AnyHTTPStatusIsReachable(t *testing.T) {
	statusCodes := []int{200, 201, 301, 400, 403, 404, 500, 502, 503}

	for _, code := range statusCodes {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer server.Close()

			strategy := NewDefaultPingStrategy(server.Client())
			target := PingTarget{
				Alias: "test-agent",
				URL:   server.URL,
			}

			result := strategy.Ping(context.Background(), target)
			if !result.Reachable {
				t.Errorf("expected reachable for status %d, got unreachable: %v", code, result.Err)
			}
		})
	}
}

func TestDefaultPingStrategy_ConnectionError(t *testing.T) {
	// Use a URL that will refuse connections
	strategy := NewDefaultPingStrategy(&http.Client{Timeout: 1 * time.Second})
	target := PingTarget{
		Alias: "unreachable-agent",
		URL:   "http://127.0.0.1:1", // port 1 is unlikely to be listening
	}

	result := strategy.Ping(context.Background(), target)
	if result.Reachable {
		t.Fatal("expected unreachable, got reachable")
	}
	if result.Err == nil {
		t.Fatal("expected non-nil error for connection failure")
	}
	if result.ResponseTime != 0 {
		t.Errorf("expected zero response time for failure, got %v", result.ResponseTime)
	}
}

func TestDefaultPingStrategy_ContextTimeout(t *testing.T) {
	// Start a server that never responds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until context is canceled
		<-r.Context().Done()
	}))
	defer server.Close()

	strategy := NewDefaultPingStrategy(server.Client())
	target := PingTarget{
		Alias: "slow-agent",
		URL:   server.URL,
	}

	// Use a very short timeout to make the test fast
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := strategy.Ping(ctx, target)
	if result.Reachable {
		t.Fatal("expected unreachable due to timeout, got reachable")
	}
	if result.Err == nil {
		t.Fatal("expected non-nil error for timeout")
	}
}

func TestDefaultPingStrategy_TrailingSlashHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-card.json" {
			t.Errorf("expected path /.well-known/agent-card.json, got %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	strategy := NewDefaultPingStrategy(server.Client())

	// URL with trailing slash
	target := PingTarget{
		Alias: "test-agent",
		URL:   server.URL + "/",
	}

	result := strategy.Ping(context.Background(), target)
	if !result.Reachable {
		t.Fatalf("expected reachable with trailing slash URL, got unreachable: %v", result.Err)
	}
}

func TestBuildPingURL(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		pingEndpoint string
		expected     string
	}{
		{
			name:         "default endpoint no trailing slash",
			baseURL:      "http://example.com",
			pingEndpoint: "",
			expected:     "http://example.com/.well-known/agent-card.json",
		},
		{
			name:         "default endpoint with trailing slash",
			baseURL:      "http://example.com/",
			pingEndpoint: "",
			expected:     "http://example.com/.well-known/agent-card.json",
		},
		{
			name:         "custom endpoint with leading slash",
			baseURL:      "http://example.com",
			pingEndpoint: "/health",
			expected:     "http://example.com/health",
		},
		{
			name:         "custom endpoint without leading slash",
			baseURL:      "http://example.com",
			pingEndpoint: "health",
			expected:     "http://example.com/health",
		},
		{
			name:         "custom endpoint with trailing slash on base",
			baseURL:      "http://example.com/",
			pingEndpoint: "/health/check",
			expected:     "http://example.com/health/check",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := buildPingURL(tc.baseURL, tc.pingEndpoint)
			if result != tc.expected {
				t.Errorf("buildPingURL(%q, %q) = %q, want %q", tc.baseURL, tc.pingEndpoint, result, tc.expected)
			}
		})
	}
}
