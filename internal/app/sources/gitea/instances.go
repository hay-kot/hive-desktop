package gitea

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
)

// InstanceStore persists each connected account's non-secret binding — the
// instance base URL — keyed by credential ref. Binding the host to the account
// at connect time, rather than letting a node name one, is what stops a node
// from sending an account's token to an arbitrary server.
type InstanceStore struct {
	path string
	mu   sync.Mutex
}

func NewInstanceStore(path string) *InstanceStore {
	return &InstanceStore{path: path}
}

// Binding is what a poll needs to address one connected account.
type Binding struct {
	URL string `json:"url"`
	// Login is the authenticated user the token belongs to, kept for display;
	// the account half of the ref is host-qualified and is not it.
	Login string `json:"login,omitempty"`
	// Version is the instance version recorded at connect time. Informational:
	// nothing branches on it, but it is what a support question asks for.
	Version string `json:"version,omitempty"`
}

// Get returns the stored binding. A zero Binding means nothing is stored — the
// ordinary "not connected" state, not an error.
func (s *InstanceStore) Get(ref credentials.Ref) (Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return Binding{}, err
	}
	return entries[ref.String()], nil
}

func (s *InstanceStore) Set(ref credentials.Ref, binding Binding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	entries[ref.String()] = binding
	return s.write(entries)
}

func (s *InstanceStore) Delete(ref credentials.Ref) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	delete(entries, ref.String())
	return s.write(entries)
}

func (s *InstanceStore) load() (map[string]Binding, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Binding{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gitea instances: read %s: %w", s.path, err)
	}
	var entries map[string]Binding
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("gitea instances: parse %s: %w", s.path, err)
	}
	if entries == nil {
		entries = map[string]Binding{}
	}
	return entries, nil
}

// write replaces the file atomically: a temp file in the same directory, then a
// rename, so a crash mid-write cannot truncate the store.
func (s *InstanceStore) write(entries map[string]Binding) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("gitea instances: create dir: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("gitea instances: encode: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("gitea instances: write temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("gitea instances: replace: %w", err)
	}
	return nil
}
