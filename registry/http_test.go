package registry

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHeaderRoundTripper_InjectsHeaders(t *testing.T) {
	var received http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := func() map[string]string {
		return map[string]string{"Authorization": "Bearer token123"}
	}
	client := HTTPClientWithHeaderProvider(http.DefaultClient, provider)

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := received.Get("Authorization"); got != "Bearer token123" {
		t.Errorf("expected Authorization header 'Bearer token123', got %q", got)
	}
}

func TestHeaderRoundTripper_ProviderCalledPerRequest(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := func() map[string]string {
		callCount.Add(1)
		return map[string]string{"X-Count": "value"}
	}
	client := HTTPClientWithHeaderProvider(http.DefaultClient, provider)

	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		_, err := client.Do(req)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
	}

	if got := callCount.Load(); got != 3 {
		t.Errorf("expected provider called 3 times, got %d", got)
	}
}

func TestHeaderRoundTripper_DynamicHeaderRefresh(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	currentToken := "token-v1"
	provider := func() map[string]string {
		return map[string]string{"Authorization": "Bearer " + currentToken}
	}
	client := HTTPClientWithHeaderProvider(http.DefaultClient, provider)

	// First request uses initial token.
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, _ = client.Do(req)
	if receivedAuth != "Bearer token-v1" {
		t.Errorf("first request: expected 'Bearer token-v1', got %q", receivedAuth)
	}

	// Update token externally (simulates registry update).
	currentToken = "token-v2"

	// Second request picks up rotated token without rebuilding client.
	req, _ = http.NewRequest(http.MethodGet, server.URL, nil)
	_, _ = client.Do(req)
	if receivedAuth != "Bearer token-v2" {
		t.Errorf("second request: expected 'Bearer token-v2', got %q", receivedAuth)
	}
}

func TestHeaderRoundTripper_SkipsProtocolHeaders(t *testing.T) {
	var received http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := func() map[string]string {
		return map[string]string{
			"Content-Type":  "text/plain",
			"Accept":        "text/html",
			"Authorization": "Bearer ok",
		}
	}
	client := HTTPClientWithHeaderProvider(http.DefaultClient, provider)

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Protocol headers should NOT be overridden.
	if got := received.Get("Content-Type"); got == "text/plain" {
		t.Error("Content-Type should not be overridden by provider")
	}
	if got := received.Get("Accept"); got == "text/html" {
		t.Error("Accept should not be overridden by provider")
	}
	// Non-protocol headers should be injected.
	if got := received.Get("Authorization"); got != "Bearer ok" {
		t.Errorf("expected Authorization 'Bearer ok', got %q", got)
	}
}

func TestHTTPClientForAgent_NoHeaders(t *testing.T) {
	base := &http.Client{}
	entry := &RegisteredAgent{Headers: nil}
	result := HTTPClientForAgent(base, entry)
	if result != base {
		t.Error("expected base client returned when entry has no headers")
	}
}

func TestHTTPClientForAgent_WithHeaders(t *testing.T) {
	var received http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	base := &http.Client{}
	entry := &RegisteredAgent{Headers: map[string]string{"X-Custom": "hello"}}
	client := HTTPClientForAgent(base, entry)

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := received.Get("X-Custom"); got != "hello" {
		t.Errorf("expected X-Custom 'hello', got %q", got)
	}
}
