package mcpsrv

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hay-kot/hive-desktop/internal/app/canvas"
)

// register declares the canvas tool table, under the same rules as
// Controller.register: metadata only, thin handlers, descriptions written for
// a model.
func (ctrl *CanvasController) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:  "put_block",
		Title: "Put a canvas block",
		Description: "Create or replace one block on this chat's canvas, which Hive shows the user beside the conversation. " +
			"You choose the block id: reusing an id updates that block in place, keeping its position, while a new id appends " +
			"at the end — stable ids are how you revise a status line instead of stacking copies. kind is markdown (body " +
			"required, title optional; rendered as GitHub-flavored markdown with raw HTML escaped, not rendered) or link " +
			"(title and url required; http, https or mailto only). Answers with the whole canvas after the write, so no " +
			"separate read is needed to confirm.",
	}, ctrl.PutBlock)

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "remove_block",
		Title: "Remove a canvas block",
		Description: "Remove one block by the id you gave it in put_block, and answer with the canvas that remains. " +
			"An id that is not on the canvas is not_found — nothing is ever reported removed that was not there.",
	}, ctrl.RemoveBlock)

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "clear_canvas",
		Title: "Clear the canvas",
		Description: "Remove every block from this chat's canvas at once and answer with the now-empty canvas. The canvas " +
			"itself survives — put_block after a clear starts a fresh layout in the same pane. Prefer put_block with stable " +
			"ids for revisions; clear is for abandoning a layout wholesale.",
	}, ctrl.ClearCanvas)

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "read_canvas",
		Title: "Read the canvas",
		Description: "Read this chat's canvas exactly as the user sees it: every block in order, with your ids, kinds and " +
			"content. A chat that has never had a block answers an empty canvas, not an error — empty and not_found are " +
			"different answers (not_found means the session id itself is wrong). Use this to re-orient after a long " +
			"conversation instead of assuming what you last wrote.",
	}, ctrl.ReadCanvas)
}

type canvasSessionInput struct {
	Session int64 `json:"session" jsonschema:"The chat session this canvas belongs to. Read it from this process's HIVE_AGENT_SESSION environment variable — Hive set it when it launched this chat. Never guess or reuse another value. If the variable is unset, say so instead of calling."`
}

type putBlockInput struct {
	Session int64  `json:"session"         jsonschema:"The chat session this canvas belongs to. Read it from this process's HIVE_AGENT_SESSION environment variable — Hive set it when it launched this chat. Never guess or reuse another value. If the variable is unset, say so instead of calling."`
	ID      string `json:"id"              jsonschema:"Your name for the block. Reusing an id updates that block in place; a new id appends."`
	Kind    string `json:"kind"            jsonschema:"markdown or link."`
	Title   string `json:"title,omitempty" jsonschema:"Heading shown above a markdown body (optional); the visible text of a link (required)."`
	Body    string `json:"body,omitempty"  jsonschema:"The markdown source of a markdown block."`
	URL     string `json:"url,omitempty"   jsonschema:"The target of a link block; http, https or mailto only."`
}

type removeBlockInput struct {
	Session int64  `json:"session" jsonschema:"The chat session this canvas belongs to. Read it from this process's HIVE_AGENT_SESSION environment variable — Hive set it when it launched this chat. Never guess or reuse another value. If the variable is unset, say so instead of calling."`
	ID      string `json:"id"      jsonschema:"The id of the block to remove, as given to put_block."`
}

type canvasBlock struct {
	ID        string `json:"id"              jsonschema:"The agent-chosen id put_block was called with."`
	Kind      string `json:"kind"            jsonschema:"markdown or link."`
	Title     string `json:"title,omitempty"`
	Body      string `json:"body,omitempty"  jsonschema:"The markdown source of a markdown block."`
	URL       string `json:"url,omitempty"   jsonschema:"The target of a link block."`
	CreatedAt int64  `json:"createdAt"       jsonschema:"Unix milliseconds when the block first appeared."`
	UpdatedAt int64  `json:"updatedAt"`
}

type canvasResult struct {
	Workspace string        `json:"workspace" jsonschema:"The workspace this chat belongs to."`
	Session   int64         `json:"session"`
	CreatedAt int64         `json:"createdAt"`
	UpdatedAt int64         `json:"updatedAt"`
	Blocks    []canvasBlock `json:"blocks"    jsonschema:"Every block, in display order."`
}

func (ctrl *CanvasController) PutBlock(ctx context.Context, _ *mcp.CallToolRequest, in putBlockInput) (*mcp.CallToolResult, canvasResult, error) {
	c, err := ctrl.core.Canvas.PutBlock(ctx, in.Session, canvas.Block{
		ID: in.ID, Kind: in.Kind, Title: in.Title, Body: in.Body, URL: in.URL,
	})
	if err != nil {
		return nil, canvasResult{}, ctrl.toolError(err)
	}
	return nil, canvasResultFrom(c), nil
}

func (ctrl *CanvasController) RemoveBlock(ctx context.Context, _ *mcp.CallToolRequest, in removeBlockInput) (*mcp.CallToolResult, canvasResult, error) {
	c, err := ctrl.core.Canvas.RemoveBlock(ctx, in.Session, in.ID)
	if err != nil {
		return nil, canvasResult{}, ctrl.toolError(err)
	}
	return nil, canvasResultFrom(c), nil
}

func (ctrl *CanvasController) ClearCanvas(ctx context.Context, _ *mcp.CallToolRequest, in canvasSessionInput) (*mcp.CallToolResult, canvasResult, error) {
	c, err := ctrl.core.Canvas.Clear(ctx, in.Session)
	if err != nil {
		return nil, canvasResult{}, ctrl.toolError(err)
	}
	return nil, canvasResultFrom(c), nil
}

func (ctrl *CanvasController) ReadCanvas(ctx context.Context, _ *mcp.CallToolRequest, in canvasSessionInput) (*mcp.CallToolResult, canvasResult, error) {
	c, err := ctrl.core.Canvas.Get(ctx, in.Session)
	if err != nil {
		return nil, canvasResult{}, ctrl.toolError(err)
	}
	return nil, canvasResultFrom(c), nil
}

func canvasResultFrom(c canvas.Canvas) canvasResult {
	blocks := make([]canvasBlock, 0, len(c.Blocks))
	for _, b := range c.Blocks {
		blocks = append(blocks, canvasBlock{
			ID: b.ID, Kind: b.Kind, Title: b.Title, Body: b.Body, URL: b.URL,
			CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
		})
	}
	return canvasResult{
		Workspace: c.Workspace, Session: c.Session,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Blocks: blocks,
	}
}
