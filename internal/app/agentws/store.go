package agentws

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const (
	libraryFileName  = "mcps.yaml"
	manifestFileName = "agent-workspace.yaml"
)

// WorkspaceStatus is one workspace directory's load outcome, keyed by the
// directory name. A valid manifest carries Workspace; a broken one carries
// Err and the last-good Workspace, so a listing UI shows why it broke rather
// than having it vanish.
type WorkspaceStatus struct {
	Dir       string
	Workspace Workspace
	Valid     bool
	Err       error
}

// LibraryStatus is mcps.yaml's own outcome. It is separate from the workspace
// statuses because it has no directory: a broken library is one problem about
// one file, not a problem about a workspace.
type LibraryStatus struct {
	Library Library
	Valid   bool
	Err     error
}

// Store holds a per-workspace last-good snapshot of the workspace root,
// reloading on demand (Reload) or from a Watcher. It follows flow.FlowStore
// rather than actions.ActionStore: the root is a directory of N independently
// authored manifests, not one file, so one broken manifest must not freeze
// the whole set. Unlike FlowStore, a broken entry keeps its own previous
// value rather than dropping out — see WorkspaceStatus.
//
// Thread-safe: every method takes the same mutex.
type Store struct {
	root string

	mu       sync.Mutex
	library  LibraryStatus
	statuses map[string]WorkspaceStatus
}

// NewStore returns a store over root (typically settings.Paths.AgentWorkspacesDir).
// Nothing is read from disk until the first Reload; before it, reads report an
// empty snapshot.
func NewStore(root string) *Store {
	return &Store{root: root, statuses: map[string]WorkspaceStatus{}}
}

// Root returns the workspace root this store was constructed over.
func (s *Store) Root() string { return s.root }

// Reload re-reads mcps.yaml and every workspace directory under the root. It
// errors only when the root directory itself cannot be read (and a missing
// root is not that: it just means nothing has been created yet, so this
// reports an empty, valid snapshot). Per-file failures never make Reload
// error; they land in Library()/Statuses() instead.
func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reloadLocked()
}

// Statuses returns one WorkspaceStatus per recognized workspace directory,
// sorted by Dir — valid and broken alike, for a listing UI that must surface
// why an entry broke.
func (s *Store) Statuses() []WorkspaceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]WorkspaceStatus, 0, len(s.statuses))
	for _, st := range s.statuses {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}

// Library returns mcps.yaml's last-good outcome.
func (s *Store) Library() LibraryStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.library
}

func (s *Store) reloadLocked() error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("agentws: read workspace root %s: %w", s.root, err)
		}
		entries = nil
	}

	s.library = s.reloadLibraryLocked()

	statuses := make(map[string]WorkspaceStatus, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == libraryFileName || !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(s.root, name, manifestFileName)
		ws, wsErr := LoadWorkspace(manifestPath)
		if wsErr != nil {
			if errors.Is(wsErr, os.ErrNotExist) {
				// No manifest: this directory is not a workspace (yet), and
				// that is not an error (spec §14) — nor does it keep a stale
				// entry around from before the manifest was removed.
				continue
			}
			prev := s.statuses[name].Workspace
			prev.Dir = name
			statuses[name] = WorkspaceStatus{Dir: name, Workspace: prev, Valid: false, Err: wsErr}
			continue
		}
		statuses[name] = WorkspaceStatus{Dir: name, Workspace: ws, Valid: true}
	}
	s.statuses = statuses
	return nil
}

// reloadLibraryLocked loads mcps.yaml, keeping the previous last-good Library
// when the file is present but broken. A missing file is a valid, empty
// library — mcps.yaml is normally seeded, but nothing here depends on that.
func (s *Store) reloadLibraryLocked() LibraryStatus {
	lib, err := LoadLibrary(filepath.Join(s.root, libraryFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return LibraryStatus{Library: Library{}, Valid: true}
		}
		return LibraryStatus{Library: s.library.Library, Valid: false, Err: err}
	}
	return LibraryStatus{Library: lib, Valid: true}
}
