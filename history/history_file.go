package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Compile-time check that FileBackend implements HistoryBackend.
var _ Backend = (*FileBackend)(nil)

// FileBackend persists history entries to JSON files in a directory.
// Each agent alias gets a separate file: <dir>/<alias>.json
// It is safe for concurrent access using per-alias locks, allowing
// operations on different aliases to proceed in parallel.
type FileBackend struct {
	mu    sync.Mutex // protects the locks map
	locks map[string]*sync.RWMutex
	dir   string
	depth int
}

// NewFileBackend creates a filesystem backend storing files in dir.
// The directory is created on first write if it doesn't exist.
// Returns an error if depth is less than 1.
func NewFileBackend(dir string, depth int) (*FileBackend, error) {
	if depth < 1 {
		return nil, fmt.Errorf("file backend depth must be at least 1, got %d", depth)
	}
	return &FileBackend{
		locks: make(map[string]*sync.RWMutex),
		dir:   dir,
		depth: depth,
	}, nil
}

// aliasLock returns the per-alias RWMutex, creating one if needed.
func (f *FileBackend) aliasLock(alias string) *sync.RWMutex {
	f.mu.Lock()
	defer f.mu.Unlock()
	lock, ok := f.locks[alias]
	if !ok {
		lock = &sync.RWMutex{}
		f.locks[alias] = lock
	}
	return lock
}

// aliasPath returns the file path for a given alias.
func (f *FileBackend) aliasPath(alias string) string {
	return filepath.Join(f.dir, alias+".json")
}

// ensureDir creates the directory if it doesn't exist.
func (f *FileBackend) ensureDir() error {
	return os.MkdirAll(f.dir, 0o755)
}

// readEntries reads and deserializes entries from the alias file.
// Returns an empty slice if the file doesn't exist.
func (f *FileBackend) readEntries(alias string) ([]Entry, error) {
	data, err := os.ReadFile(f.aliasPath(alias))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []Entry{}, nil
		}
		return nil, fmt.Errorf("reading history file for %q: %w", alias, err)
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("unmarshaling history for %q: %w", alias, err)
	}
	return entries, nil
}

// writeEntries serializes and writes entries to the alias file.
func (f *FileBackend) writeEntries(alias string, entries []Entry) error {
	if err := f.ensureDir(); err != nil {
		return fmt.Errorf("creating history directory: %w", err)
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshaling history for %q: %w", alias, err)
	}

	if err := os.WriteFile(f.aliasPath(alias), data, 0o644); err != nil {
		return fmt.Errorf("writing history file for %q: %w", alias, err)
	}
	return nil
}

// Append reads the current file, appends the entry, enforces depth, and writes back.
func (f *FileBackend) Append(_ context.Context, alias string, entry Entry) error {
	lock := f.aliasLock(alias)
	lock.Lock()
	defer lock.Unlock()

	entries, err := f.readEntries(alias)
	if err != nil {
		return err
	}

	if len(entries) >= f.depth {
		// Evict oldest entries to make room.
		entries = entries[len(entries)-f.depth+1:]
	}
	entries = append(entries, entry)

	return f.writeEntries(alias, entries)
}

// List reads and deserializes the file, returns empty slice if file doesn't exist.
func (f *FileBackend) List(_ context.Context, alias string) ([]Entry, error) {
	lock := f.aliasLock(alias)
	lock.RLock()
	defer lock.RUnlock()

	return f.readEntries(alias)
}

// Delete removes the history file for the given alias.
func (f *FileBackend) Delete(_ context.Context, alias string) error {
	lock := f.aliasLock(alias)
	lock.Lock()
	defer lock.Unlock()

	err := os.Remove(f.aliasPath(alias))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("deleting history file for %q: %w", alias, err)
	}
	return nil
}

// Clear removes the history file for the given alias.
// Semantically identical to Delete — provided for naming clarity at call sites.
func (f *FileBackend) Clear(_ context.Context, alias string) error {
	lock := f.aliasLock(alias)
	lock.Lock()
	defer lock.Unlock()

	err := os.Remove(f.aliasPath(alias))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clearing history file for %q: %w", alias, err)
	}
	return nil
}

// Close is a no-op for the file backend (no persistent handles held between operations).
func (f *FileBackend) Close(_ context.Context) error {
	return nil
}
