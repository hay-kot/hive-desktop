package httpapi

import (
	"net/http"

	"github.com/hay-kot/criterio"
	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app/canvas"
)

// The canvas surface here is reads only: writes arrive exclusively through
// the hive-canvas MCP tools, so the pane can never race the agent through a
// second mutation path (ADR the-canvas-is-a-per-chat-file-served-over-its-own-mcp-entry).

type agentCanvasBlock struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type agentCanvasView struct {
	Workspace string             `json:"workspace"`
	Session   int64              `json:"session"`
	CreatedAt int64              `json:"createdAt"`
	UpdatedAt int64              `json:"updatedAt"`
	Blocks    []agentCanvasBlock `json:"blocks"`
}

type agentCanvasMeta struct {
	Workspace  string `json:"workspace"`
	Session    int64  `json:"session"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
	BlockCount int    `json:"blockCount"`
}

type agentCanvasRequest struct {
	Session int64 `json:"session"`
}

func (b agentCanvasRequest) Validate() error {
	return criterio.Run("session", b.Session, criterio.Positive[int64]())
}

// AgentCanvas reads one chat session's canvas.
func (ctrl *Controller) AgentCanvas(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentCanvasRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	c, err := ctrl.core.Canvas.Get(r.Context(), body.Session)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, toAgentCanvasView(c))
}

type agentCanvasListRequest struct {
	Workspace string `json:"workspace"`
}

func (b agentCanvasListRequest) Validate() error {
	return criterio.Run("workspace", b.Workspace, criterio.Required)
}

type agentCanvasListResponse struct {
	Canvases []agentCanvasMeta `json:"canvases"`
}

// AgentCanvasList lists a workspace's canvases, most recently updated first.
func (ctrl *Controller) AgentCanvasList(w http.ResponseWriter, r *http.Request) error {
	body, err := terminalBody[agentCanvasListRequest](ctrl, w, r)
	if err != nil {
		return err
	}
	metas, err := ctrl.core.Canvas.ListForWorkspace(r.Context(), body.Workspace)
	if err != nil {
		return err
	}
	views := make([]agentCanvasMeta, 0, len(metas))
	for _, m := range metas {
		views = append(views, agentCanvasMeta{
			Workspace: m.Workspace, Session: m.Session,
			CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, BlockCount: m.BlockCount,
		})
	}
	return server.JSON(w, http.StatusOK, agentCanvasListResponse{Canvases: views})
}

func toAgentCanvasView(c canvas.Canvas) agentCanvasView {
	blocks := make([]agentCanvasBlock, 0, len(c.Blocks))
	for _, b := range c.Blocks {
		blocks = append(blocks, agentCanvasBlock{
			ID: b.ID, Kind: b.Kind, Title: b.Title, Body: b.Body, URL: b.URL,
			CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
		})
	}
	return agentCanvasView{
		Workspace: c.Workspace, Session: c.Session,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Blocks: blocks,
	}
}
