package grafana

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
)

// StackStore persists each connected stack's non-secret base URL, keyed by
// credential ref. Binding the URL to the account here — rather than letting a
// node choose a host — is what stops a node from pairing an account's token with
// an arbitrary host.
type StackStore struct {
	path string
	mu   sync.Mutex
}

func NewStackStore(path string) *StackStore {
	return &StackStore{path: path}
}

type stackEntry struct {
	URL string `json:"url"`
}

// URL returns the stored base URL for a stack, or "" when none is stored — the
// ordinary "not connected" state, not an error.
func (s *StackStore) URL(ref credentials.Ref) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return "", err
	}
	return entries[ref.String()].URL, nil
}

func (s *StackStore) Set(ref credentials.Ref, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	entries[ref.String()] = stackEntry{URL: url}
	return s.write(entries)
}

func (s *StackStore) Delete(ref credentials.Ref) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	delete(entries, ref.String())
	return s.write(entries)
}

func (s *StackStore) load() (map[string]stackEntry, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]stackEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("grafana stacks: read %s: %w", s.path, err)
	}
	var entries map[string]stackEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("grafana stacks: parse %s: %w", s.path, err)
	}
	if entries == nil {
		entries = map[string]stackEntry{}
	}
	return entries, nil
}

// write replaces the file atomically: a temp file in the same directory, then a
// rename, so a crash mid-write cannot truncate the store.
func (s *StackStore) write(entries map[string]stackEntry) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("grafana stacks: create dir: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("grafana stacks: encode: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("grafana stacks: write temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("grafana stacks: replace: %w", err)
	}
	return nil
}
