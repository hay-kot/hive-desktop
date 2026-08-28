package app

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/canvas"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// maxCanvasBodyBytes caps one markdown block's body so a single tool call
// cannot make the pane unrenderable.
const maxCanvasBodyBytes = 256 * 1024

const (
	maxCanvasBlockIDLength = 200
	maxCanvasTitleLength   = 200
)

// canvasSessionResolver is the one store read every canvas mutation starts
// with: the session record is the authority on which workspace a canvas
// belongs to, so an agent never names the workspace itself.
type canvasSessionResolver interface {
	GetAgentWorkspaceSession(ctx context.Context, id int64) (store.AgentWorkspaceSession, bool, error)
}

// CanvasService is the workspace's canvases: named, agent-written artifacts
// the Agents area shows beside the conversation. Writes arrive only through
// the hive-canvas MCP tools; the frontend reads
// (ADR canvases-are-named-files-in-the-workspace-folder-served-over-their-own-mcp-entry).
type CanvasService struct {
	store     *canvas.Store
	sessions  canvasSessionResolver
	onUpdated func(workspace string, session int64)
	onToggled func(workspace string, session int64, name string, open bool)
}

func newCanvasService(store *canvas.Store, sessions canvasSessionResolver, onUpdated func(string, int64), onToggled func(string, int64, string, bool)) *CanvasService {
	return &CanvasService{store: store, sessions: sessions, onUpdated: onUpdated, onToggled: onToggled}
}

// Get returns one canvas in the calling session's workspace. A name nothing
// was ever written under is not_found — unlike the pane, an agent asking for
// a canvas by name should learn the name is wrong, not see a blank surface.
func (s *CanvasService) Get(ctx context.Context, session int64, name string) (canvas.Canvas, error) {
	rec, err := s.resolve(ctx, session)
	if err != nil {
		return canvas.Canvas{}, err
	}
	c, ok, err := s.store.Load(rec.Workspace, name)
	if err != nil {
		return canvas.Canvas{}, s.storeError(err, name)
	}
	if !ok {
		return canvas.Canvas{}, Errorf(KindNotFound, "no canvas named %q in this workspace", name)
	}
	return c, nil
}

// PutBlock creates or replaces one block, creating the canvas on its first
// write. A non-empty title renames the canvas; empty leaves the stored one.
// Returns the canvas after the write.
func (s *CanvasService) PutBlock(ctx context.Context, session int64, name, title string, b canvas.Block) (canvas.Canvas, error) {
	rec, err := s.resolve(ctx, session)
	if err != nil {
		return canvas.Canvas{}, err
	}
	if len(title) > maxCanvasTitleLength {
		return canvas.Canvas{}, Errorf(KindInvalid, "canvas title is too long (%d chars max)", maxCanvasTitleLength)
	}
	if err := validateBlock(&b); err != nil {
		return canvas.Canvas{}, err
	}
	c, err := s.store.Upsert(rec.Workspace, name, session, title, b)
	if err != nil {
		return canvas.Canvas{}, s.storeError(err, name)
	}
	s.notify(rec.Workspace, session)
	return c, nil
}

// RemoveBlock deletes one block by id and returns the canvas that remains.
func (s *CanvasService) RemoveBlock(ctx context.Context, session int64, name, blockID string) (canvas.Canvas, error) {
	rec, err := s.resolve(ctx, session)
	if err != nil {
		return canvas.Canvas{}, err
	}
	c, removed, err := s.store.Remove(rec.Workspace, name, blockID)
	if err != nil {
		return canvas.Canvas{}, s.storeError(err, name)
	}
	if !removed {
		return canvas.Canvas{}, Errorf(KindNotFound, "no block %q on canvas %q", blockID, name)
	}
	s.notify(rec.Workspace, session)
	return c, nil
}

// Clear removes every block at once; the canvas, its title and its file
// survive.
func (s *CanvasService) Clear(ctx context.Context, session int64, name string) (canvas.Canvas, error) {
	rec, err := s.resolve(ctx, session)
	if err != nil {
		return canvas.Canvas{}, err
	}
	c, err := s.store.Clear(rec.Workspace, name)
	if err != nil {
		return canvas.Canvas{}, s.storeError(err, name)
	}
	s.notify(rec.Workspace, session)
	return c, nil
}

// SetPaneOpen asks the UI to open or close the canvas pane beside the
// calling chat — UI intent, applied only while the user is viewing that
// chat, so an agent can surface what it made without ever dragging the user
// away from something else. Opening with a name requires that canvas to
// exist; empty leaves the pane's own pick.
func (s *CanvasService) SetPaneOpen(ctx context.Context, session int64, name string, open bool) error {
	rec, err := s.resolve(ctx, session)
	if err != nil {
		return err
	}
	if open && name != "" {
		if _, err := s.Get(ctx, session, name); err != nil {
			return err
		}
	}
	if s.onToggled != nil {
		s.onToggled(rec.Workspace, session, name, open)
	}
	return nil
}

// Delete removes one canvas file entirely.
func (s *CanvasService) Delete(ctx context.Context, session int64, name string) error {
	rec, err := s.resolve(ctx, session)
	if err != nil {
		return err
	}
	existed, err := s.store.Delete(rec.Workspace, name)
	if err != nil {
		return s.storeError(err, name)
	}
	if !existed {
		return Errorf(KindNotFound, "no canvas named %q in this workspace", name)
	}
	s.notify(rec.Workspace, session)
	return nil
}

// List returns the calling session's workspace canvases, most recently
// updated first.
func (s *CanvasService) List(ctx context.Context, session int64) ([]canvas.Meta, error) {
	rec, err := s.resolve(ctx, session)
	if err != nil {
		return nil, err
	}
	return s.ListForWorkspace(ctx, rec.Workspace)
}

// GetForWorkspace is the pane's read: workspace-addressed, and a name
// nothing was written under answers an empty canvas rather than an error, so
// the pane never flashes a failure for a canvas that was deleted under it.
func (s *CanvasService) GetForWorkspace(_ context.Context, dir, name string) (canvas.Canvas, error) {
	c, ok, err := s.store.Load(dir, name)
	if err != nil {
		return canvas.Canvas{}, s.storeError(err, name)
	}
	if !ok {
		return canvas.Canvas{Workspace: dir, Name: name, Blocks: []canvas.Block{}}, nil
	}
	return c, nil
}

// MarkdownForWorkspace renders one canvas as a standalone markdown document
// — the pane's copy action. A name nothing was written under is not_found:
// exporting nothing is a mistake worth surfacing, unlike showing it.
func (s *CanvasService) MarkdownForWorkspace(_ context.Context, dir, name string) (string, error) {
	c, ok, err := s.store.Load(dir, name)
	if err != nil {
		return "", s.storeError(err, name)
	}
	if !ok {
		return "", Errorf(KindNotFound, "no canvas named %q in this workspace", name)
	}
	return canvas.Markdown(c), nil
}

// ExportForWorkspace writes one canvas's markdown rendering to path — the
// pane's save action, with path coming from the native save dialog, which is
// why it must already be absolute.
func (s *CanvasService) ExportForWorkspace(ctx context.Context, dir, name, path string) error {
	markdown, err := s.MarkdownForWorkspace(ctx, dir, name)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(path) {
		return Errorf(KindInvalid, "export path must be absolute")
	}
	if err := os.WriteFile(path, []byte(markdown), 0o644); err != nil {
		return Wrap(err, KindInternal, "writing export for canvas %q", name)
	}
	return nil
}

// ListForWorkspace returns a workspace's canvas metadata, most recently
// updated first. A canvas whose creating chat is gone still lists — the
// artifact outlives the chat that produced it.
func (s *CanvasService) ListForWorkspace(_ context.Context, dir string) ([]canvas.Meta, error) {
	metas, err := s.store.List(dir)
	if err != nil {
		if errors.Is(err, canvas.ErrInvalidWorkspace) {
			return nil, Errorf(KindInvalid, "workspace %q is not a valid workspace directory name", dir)
		}
		return nil, Wrap(err, KindInternal, "listing canvases for workspace %q", dir)
	}
	return metas, nil
}

func (s *CanvasService) resolve(ctx context.Context, session int64) (store.AgentWorkspaceSession, error) {
	if s.sessions == nil {
		return store.AgentWorkspaceSession{}, Errorf(KindUnavailable, "canvas is not available in this build")
	}
	rec, ok, err := s.sessions.GetAgentWorkspaceSession(ctx, session)
	if err != nil {
		return store.AgentWorkspaceSession{}, Wrap(err, KindInternal, "loading session %d", session)
	}
	if !ok {
		return store.AgentWorkspaceSession{}, Errorf(KindNotFound, "session %d not found", session)
	}
	return rec, nil
}

// storeError maps the store's sentinel errors onto typed service errors, so
// a bad name or a missing canvas reads as the caller's mistake, not an
// internal failure.
func (s *CanvasService) storeError(err error, name string) error {
	switch {
	case errors.Is(err, canvas.ErrInvalidName):
		return Errorf(KindInvalid, "canvas name %q is not allowed: use a short lowercase name like release-notes (letters, digits, dots, hyphens, underscores)", name)
	case errors.Is(err, canvas.ErrInvalidWorkspace):
		return Errorf(KindInvalid, "workspace is not a valid workspace directory name")
	case errors.Is(err, canvas.ErrNotFound):
		return Errorf(KindNotFound, "no canvas named %q in this workspace", name)
	default:
		return Wrap(err, KindInternal, "canvas %q", name)
	}
}

func (s *CanvasService) notify(workspace string, session int64) {
	if s.onUpdated != nil {
		s.onUpdated(workspace, session)
	}
}

func validateBlock(b *canvas.Block) error {
	b.ID = strings.TrimSpace(b.ID)
	if b.ID == "" {
		return Errorf(KindInvalid, "a block needs an id")
	}
	if len(b.ID) > maxCanvasBlockIDLength {
		return Errorf(KindInvalid, "block id is too long (%d chars max)", maxCanvasBlockIDLength)
	}
	switch b.Kind {
	case canvas.KindMarkdown:
		if b.Body == "" {
			return Errorf(KindInvalid, "a markdown block needs a body")
		}
		if len(b.Body) > maxCanvasBodyBytes {
			return Errorf(KindInvalid, "block body is too large (%d bytes max)", maxCanvasBodyBytes)
		}
		if b.URL != "" {
			return Errorf(KindInvalid, "a markdown block carries no url; use a link block")
		}
	case canvas.KindLink:
		if b.Title == "" {
			return Errorf(KindInvalid, "a link block needs a title")
		}
		if b.Body != "" {
			return Errorf(KindInvalid, "a link block carries no body; use a markdown block")
		}
		if err := validateLinkURL(b.URL); err != nil {
			return err
		}
	default:
		return Errorf(KindInvalid, "unknown block kind %q; use %q or %q", b.Kind, canvas.KindMarkdown, canvas.KindLink)
	}
	return nil
}

// validateLinkURL mirrors the frontend's link filter: only schemes the pane
// will actually open are accepted, so a block never renders a link the click
// handler then refuses.
func validateLinkURL(raw string) error {
	if raw == "" {
		return Errorf(KindInvalid, "a link block needs a url")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return Errorf(KindInvalid, "link url %q does not parse", raw)
	}
	switch parsed.Scheme {
	case "http", "https", "mailto":
		return nil
	default:
		return Errorf(KindInvalid, "link url scheme %q is not allowed; use http, https or mailto", parsed.Scheme)
	}
}
