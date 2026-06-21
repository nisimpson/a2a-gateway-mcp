package registry

import (
	"sync"
	"testing"
)

func TestNewContextStore(t *testing.T) {
	cs := NewContextStore()
	if cs == nil {
		t.Fatal("NewContextStore returned nil")
	}
	if cs.contexts == nil {
		t.Fatal("contexts map is nil")
	}
	if len(cs.contexts) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(cs.contexts))
	}
}

func TestContextStore_GetEmpty(t *testing.T) {
	cs := NewContextStore()
	got := cs.Get("nonexistent")
	if got != "" {
		t.Fatalf("expected empty string for missing alias, got %q", got)
	}
}

func TestContextStore_SetAndGet(t *testing.T) {
	cs := NewContextStore()
	cs.Set("agent-1", "ctx-abc")

	got := cs.Get("agent-1")
	if got != "ctx-abc" {
		t.Fatalf("expected %q, got %q", "ctx-abc", got)
	}
}

func TestContextStore_SetEmptyDoesNotModify(t *testing.T) {
	cs := NewContextStore()
	cs.Set("agent-1", "ctx-original")

	// Setting empty contextID should not modify the entry
	cs.Set("agent-1", "")

	got := cs.Get("agent-1")
	if got != "ctx-original" {
		t.Fatalf("expected %q after empty Set, got %q", "ctx-original", got)
	}
}

func TestContextStore_SetEmptyOnMissingAlias(t *testing.T) {
	cs := NewContextStore()

	// Setting empty contextID on a non-existent alias should not create an entry
	cs.Set("agent-1", "")

	got := cs.Get("agent-1")
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestContextStore_SetOverwrites(t *testing.T) {
	cs := NewContextStore()
	cs.Set("agent-1", "ctx-first")
	cs.Set("agent-1", "ctx-second")

	got := cs.Get("agent-1")
	if got != "ctx-second" {
		t.Fatalf("expected %q, got %q", "ctx-second", got)
	}
}

func TestContextStore_Delete(t *testing.T) {
	cs := NewContextStore()
	cs.Set("agent-1", "ctx-abc")
	cs.Delete("agent-1")

	got := cs.Get("agent-1")
	if got != "" {
		t.Fatalf("expected empty string after delete, got %q", got)
	}
}

func TestContextStore_DeleteNonexistent(t *testing.T) {
	cs := NewContextStore()
	// Should not panic
	cs.Delete("nonexistent")
}

func TestContextStore_ConcurrentAccess(t *testing.T) {
	cs := NewContextStore()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			alias := "agent-" + string(rune('a'+n%26))
			cs.Set(alias, "ctx-"+alias)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			alias := "agent-" + string(rune('a'+n%26))
			_ = cs.Get(alias)
		}(i)
	}

	// Concurrent deletes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			alias := "agent-" + string(rune('a'+n%26))
			cs.Delete(alias)
		}(i)
	}

	wg.Wait()
}
