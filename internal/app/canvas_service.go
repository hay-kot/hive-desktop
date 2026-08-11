package app

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/canvas"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// maxCanvasBodyBytes caps one markdown block's body so a single tool call
// cannot make the pane unrenderable.
const maxCanvasBodyBytes = 256 * 1024

const maxCanvasBlockIDLength = 200

// canvasSessionResolver is the one store read every canvas operation starts
// with: the session record is the authority on which workspace a canvas
// belongs to, so a caller never names the workspace itself.
type canvasSessionResolver interface {
	GetAgentWorkspaceSession(ctx context.Context, id int64) (store.AgentWorkspaceSession, bool, error)
}

// CanvasService is the per-chat canvas: agent-written blocks the Agents area
// shows beside the conversation. Writes arrive only through the hive-canvas
// MCP tools; the frontend reads (ADR the-canvas-is-a-per-chat-file-served-over-its-own-mcp-entry).
type CanvasService struct {
	store     *canvas.Store
	sessions  canvasSessionResolver
	onUpdated func(workspace string, session int64)
}

func newCanvasService(store *canvas.Store, sessions canvasSessionResolver, onUpdated func(string, int64)) *CanvasService {
	return &CanvasService{store: store, sessions: sessions, onUpdated: onUpdated}
}

// Get returns a session's canvas. A session that exists but has never been
// written to answers an empty canvas — emptiness is an answer; not_found
// means the session id itself resolves to nothing.
func (s *CanvasService) Get(ctx context.Context, session int64) (canvas.Canvas, error) {
	rec, err := s.resolve(ctx, session)
	if err != nil {
		return canvas.Canvas{}, err
	}
	c, ok, err := s.store.Load(rec.Workspace, session)
	if err != nil {
		return canvas.Canvas{}, Wrap(err, KindInternal, "loading canvas for session %d", session)
	}
	if !ok {
		return canvas.Canvas{Workspace: rec.Workspace, Session: session, Blocks: []canvas.Block{}}, nil
	}
	return c, nil
}

// PutBlock creates or replaces one block on a session's canvas and returns
// the canvas after the write.
func (s *CanvasService) PutBlock(ctx context.Context, session int64, b canvas.Block) (canvas.Canvas, error) {
	rec, err := s.resolve(ctx, session)
	if err != nil {
		return canvas.Canvas{}, err
	}
	if err := validateBlock(&b); err != nil {
		return canvas.Canvas{}, err
	}
	c, err := s.store.Upsert(rec.Workspace, session, b)
	if err != nil {
		return canvas.Canvas{}, Wrap(err, KindInternal, "writing block %q for session %d", b.ID, session)
	}
	s.notify(rec.Workspace, session)
	return c, nil
}

// RemoveBlock deletes one block by id and returns the canvas that remains.
func (s *CanvasService) RemoveBlock(ctx context.Context, session int64, blockID string) (canvas.Canvas, error) {
	rec, err := s.resolve(ctx, session)
	if err != nil {
		return canvas.Canvas{}, err
	}
	c, removed, err := s.store.Remove(rec.Workspace, session, blockID)
	if err != nil {
		return canvas.Canvas{}, Wrap(err, KindInternal, "removing block %q for session %d", blockID, session)
	}
	if !removed {
		return canvas.Canvas{}, Errorf(KindNotFound, "no block %q on this canvas", blockID)
	}
	s.notify(rec.Workspace, session)
	return c, nil
}

// Clear removes every block at once; the canvas itself survives.
func (s *CanvasService) Clear(ctx context.Context, session int64) (canvas.Canvas, error) {
	rec, err := s.resolve(ctx, session)
	if err != nil {
		return canvas.Canvas{}, err
	}
	c, err := s.store.Clear(rec.Workspace, session)
	if err != nil {
		return canvas.Canvas{}, Wrap(err, KindInternal, "clearing canvas for session %d", session)
	}
	s.notify(rec.Workspace, session)
	return c, nil
}

// ListForWorkspace returns a workspace's canvas metadata, most recently
// updated first. A canvas whose session record is gone still lists — the
// content outlives the chat, and the UI labels the orphan by date.
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
