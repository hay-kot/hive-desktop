// Package canvas owns the on-disk canvas tree: one JSON file per chat
// session at <root>/<workspace>/<session>.json. Content lives in files
// rather than the pipeline store so a canvas is inspectable and disposable
// without a migration (ADR the-canvas-is-a-per-chat-file-served-over-its-own-mcp-entry).
package canvas

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrInvalidWorkspace reports a workspace value that is not a single local
// path component. The store is a leaf, so it re-checks what
// validWorkspaceDir already enforced above it rather than importing anything.
var ErrInvalidWorkspace = errors.New("canvas: invalid workspace name")

const (
	KindMarkdown = "markdown"
	KindLink     = "link"
)

// Block is one entry on a canvas. Kind decides which content field is set:
// markdown carries Body, link carries URL. Timestamps are unix milliseconds,
// matching AgentWorkspaceSession.
type Block struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title,omitempty"`
	Body      string `json:"body,omitempty"`
	URL       string `json:"url,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// Canvas is one session's whole surface, in display order.
type Canvas struct {
	Workspace string  `json:"workspace"`
	Session   int64   `json:"session"`
	CreatedAt int64   `json:"createdAt"`
	UpdatedAt int64   `json:"updatedAt"`
	Blocks    []Block `json:"blocks"`
}

// Meta is one row of a workspace listing: everything the pane's picker needs
// without loading block content.
type Meta struct {
	Workspace  string `json:"workspace"`
	Session    int64  `json:"session"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
	BlockCount int    `json:"blockCount"`
}

// Store reads and writes the canvas tree under root. The mutex serializes
// read-modify-write cycles; the MCP server and the HTTP reads run in this one
// process, so no cross-process coordination is needed.
type Store struct {
	root string
	mu   sync.Mutex
	now  func() time.Time
}

func NewStore(root string) *Store {
	return &Store{root: root, now: time.Now}
}

// Load returns a session's canvas, reporting false without error when none
// has ever been written. A file that exists but cannot be parsed is an error,
// never a silently blank canvas.
func (s *Store) Load(workspace string, session int64) (Canvas, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(workspace, session)
}

// Upsert writes one block: an id already on the canvas is replaced in place,
// keeping its position and CreatedAt; a new id appends. Returns the canvas
// after the write.
func (s *Store) Upsert(workspace string, session int64, b Block) (Canvas, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok, err := s.load(workspace, session)
	if err != nil {
		return Canvas{}, err
	}
	nowMillis := s.now().UnixMilli()
	if !ok {
		c = Canvas{Workspace: workspace, Session: session, CreatedAt: nowMillis, Blocks: []Block{}}
	}

	b.CreatedAt = nowMillis
	b.UpdatedAt = nowMillis
	replaced := false
	for i, existing := range c.Blocks {
		if existing.ID == b.ID {
			b.CreatedAt = existing.CreatedAt
			c.Blocks[i] = b
			replaced = true
			break
		}
	}
	if !replaced {
		c.Blocks = append(c.Blocks, b)
	}
	c.UpdatedAt = nowMillis

	if err := s.write(c); err != nil {
		return Canvas{}, err
	}
	return c, nil
}

// Remove deletes one block by id, reporting whether it was present. A canvas
// that was never written removes nothing.
func (s *Store) Remove(workspace string, session int64, blockID string) (Canvas, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok, err := s.load(workspace, session)
	if err != nil || !ok {
		return Canvas{}, false, err
	}
	kept := c.Blocks[:0]
	removed := false
	for _, b := range c.Blocks {
		if b.ID == blockID {
			removed = true
			continue
		}
		kept = append(kept, b)
	}
	if !removed {
		return c, false, nil
	}
	c.Blocks = kept
	c.UpdatedAt = s.now().UnixMilli()
	if err := s.write(c); err != nil {
		return Canvas{}, false, err
	}
	return c, true, nil
}

// Clear empties the canvas but keeps it: the file and its CreatedAt survive,
// so a clear reads as a fresh layout in the same pane, not a deletion.
func (s *Store) Clear(workspace string, session int64) (Canvas, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok, err := s.load(workspace, session)
	if err != nil {
		return Canvas{}, err
	}
	nowMillis := s.now().UnixMilli()
	if !ok {
		c = Canvas{Workspace: workspace, Session: session, CreatedAt: nowMillis}
	}
	c.Blocks = []Block{}
	c.UpdatedAt = nowMillis
	if err := s.write(c); err != nil {
		return Canvas{}, err
	}
	return c, nil
}

// List returns a workspace's canvases, most recently updated first. A
// workspace with none — including one whose directory does not exist —
// answers empty.
func (s *Store) List(workspace string) ([]Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !validWorkspace(workspace) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidWorkspace, workspace)
	}
	entries, err := os.ReadDir(filepath.Join(s.root, workspace))
	if errors.Is(err, os.ErrNotExist) {
		return []Meta{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("canvas: list %s: %w", workspace, err)
	}

	metas := make([]Meta, 0, len(entries))
	for _, entry := range entries {
		session, ok := sessionFromFilename(entry.Name())
		if !ok {
			continue
		}
		c, found, err := s.load(workspace, session)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		metas = append(metas, Meta{
			Workspace: c.Workspace, Session: c.Session,
			CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, BlockCount: len(c.Blocks),
		})
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].UpdatedAt > metas[j].UpdatedAt })
	return metas, nil
}

// DeleteSession removes one session's canvas file; a canvas that never
// existed is not an error.
func (s *Store) DeleteSession(workspace string, session int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.path(workspace, session)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("canvas: delete %s/%d: %w", workspace, session, err)
	}
	return nil
}

// DeleteWorkspace removes a workspace's whole canvas directory.
func (s *Store) DeleteWorkspace(workspace string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !validWorkspace(workspace) {
		return fmt.Errorf("%w: %q", ErrInvalidWorkspace, workspace)
	}
	if err := os.RemoveAll(filepath.Join(s.root, workspace)); err != nil {
		return fmt.Errorf("canvas: delete workspace %s: %w", workspace, err)
	}
	return nil
}

func (s *Store) load(workspace string, session int64) (Canvas, bool, error) {
	path, err := s.path(workspace, session)
	if err != nil {
		return Canvas{}, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Canvas{}, false, nil
	}
	if err != nil {
		return Canvas{}, false, fmt.Errorf("canvas: read %s/%d: %w", workspace, session, err)
	}
	var c Canvas
	if err := json.Unmarshal(data, &c); err != nil {
		return Canvas{}, false, fmt.Errorf("canvas: parse %s/%d: %w", workspace, session, err)
	}
	if c.Blocks == nil {
		c.Blocks = []Block{}
	}
	return c, true, nil
}

// write replaces the file atomically: a temp file in the same directory, then
// a rename, so a crash mid-write cannot truncate a canvas.
func (s *Store) write(c Canvas) error {
	path, err := s.path(c.Workspace, c.Session)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("canvas: create dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("canvas: encode %s/%d: %w", c.Workspace, c.Session, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("canvas: write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("canvas: replace: %w", err)
	}
	return nil
}

func (s *Store) path(workspace string, session int64) (string, error) {
	if !validWorkspace(workspace) {
		return "", fmt.Errorf("%w: %q", ErrInvalidWorkspace, workspace)
	}
	return filepath.Join(s.root, workspace, strconv.FormatInt(session, 10)+".json"), nil
}

// validWorkspace is the one-path-component rule validWorkspaceDir enforces
// above this package.
func validWorkspace(dir string) bool {
	if dir == "" || dir == "." {
		return false
	}
	return filepath.Base(dir) == dir && filepath.IsLocal(dir)
}

func sessionFromFilename(name string) (int64, bool) {
	base, ok := strings.CutSuffix(name, ".json")
	if !ok {
		return 0, false
	}
	session, err := strconv.ParseInt(base, 10, 64)
	if err != nil {
		return 0, false
	}
	return session, true
}
