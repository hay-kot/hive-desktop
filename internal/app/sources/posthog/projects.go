package posthog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
)

// ProjectStore persists each connected project's non-secret binding — host URL
// and numeric project id — keyed by credential ref. Binding both to the
// account here, rather than letting a node choose them, is what stops a node
// from pairing an account's API key with an arbitrary host or reading a
// project that key was never connected to.
type ProjectStore struct {
	path string
	mu   sync.Mutex
}

func NewProjectStore(path string) *ProjectStore {
	return &ProjectStore{path: path}
}

// Binding is what a poll needs to address one connected project.
type Binding struct {
	URL       string `json:"url"`
	ProjectID int    `json:"projectID"`
	Name      string `json:"name,omitempty"`
}

// Get returns the stored binding for a project. A zero Binding means nothing
// is stored — the ordinary "not connected" state, not an error.
func (s *ProjectStore) Get(ref credentials.Ref) (Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return Binding{}, err
	}
	return entries[ref.String()], nil
}

func (s *ProjectStore) Set(ref credentials.Ref, binding Binding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	entries[ref.String()] = binding
	return s.write(entries)
}

func (s *ProjectStore) Delete(ref credentials.Ref) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	delete(entries, ref.String())
	return s.write(entries)
}

func (s *ProjectStore) load() (map[string]Binding, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Binding{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("posthog projects: read %s: %w", s.path, err)
	}
	var entries map[string]Binding
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("posthog projects: parse %s: %w", s.path, err)
	}
	if entries == nil {
		entries = map[string]Binding{}
	}
	return entries, nil
}

// write replaces the file atomically: a temp file in the same directory, then
// a rename, so a crash mid-write cannot truncate the store.
func (s *ProjectStore) write(entries map[string]Binding) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("posthog projects: create dir: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("posthog projects: encode: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("posthog projects: write temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("posthog projects: replace: %w", err)
	}
	return nil
}
