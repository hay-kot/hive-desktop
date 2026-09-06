package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hay-kot/hive-desktop/internal/app/canvas"
	"github.com/hay-kot/hive-desktop/internal/app/data/stores"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCanvasSessions map[int64]stores.AgentSession

func (f fakeCanvasSessions) Get(_ context.Context, id int64) (stores.AgentSession, bool, error) {
	rec, ok := f[id]
	return rec, ok, nil
}

type canvasToggle struct {
	session int64
	name    string
	open    bool
}

type canvasSignals struct {
	updates []int64
	toggles []canvasToggle
}

func testCanvasService(t *testing.T) (*CanvasService, *canvasSignals) {
	t.Helper()
	signals := &canvasSignals{}
	sessions := fakeCanvasSessions{
		1: {ID: 1, Workspace: "ws", Name: "chat", Agent: "claude"},
	}
	svc := newCanvasService(CanvasDeps{
		Store:    canvas.NewStore(t.TempDir()),
		Sessions: sessions,
		OnUpdated: func(session int64) {
			signals.updates = append(signals.updates, session)
		},
		OnToggled: func(session int64, name string, open bool) {
			signals.toggles = append(signals.toggles, canvasToggle{session, name, open})
		},
	})
	return svc, signals
}

func TestCanvasUnknownSessionIsNotFound(t *testing.T) {
	svc, _ := testCanvasService(t)
	ctx := t.Context()

	_, err := svc.Get(ctx, 99, "plan")
	assert.Equal(t, KindNotFound, KindOf(err))
	_, err = svc.PutBlock(ctx, 99, "plan", "", "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	assert.Equal(t, KindNotFound, KindOf(err))
	_, err = svc.RemoveBlock(ctx, 99, "plan", "a")
	assert.Equal(t, KindNotFound, KindOf(err))
	_, err = svc.Clear(ctx, 99, "plan")
	assert.Equal(t, KindNotFound, KindOf(err))
	err = svc.Delete(ctx, 99, "plan")
	assert.Equal(t, KindNotFound, KindOf(err))
	_, err = svc.List(ctx, 99)
	assert.Equal(t, KindNotFound, KindOf(err))
}

func TestCanvasGetUnknownNameIsNotFound(t *testing.T) {
	svc, signals := testCanvasService(t)

	_, err := svc.Get(t.Context(), 1, "plan")
	assert.Equal(t, KindNotFound, KindOf(err), "an agent asking by name should learn the name is wrong")
	assert.Empty(t, signals.updates, "a read never notifies")
}

func TestCanvasGetForWorkspaceUnwrittenAnswersEmpty(t *testing.T) {
	svc, signals := testCanvasService(t)

	c, err := svc.GetForWorkspace(t.Context(), "ws", "plan")
	require.NoError(t, err)
	assert.Equal(t, "ws", c.Workspace)
	assert.Equal(t, "plan", c.Name)
	assert.NotNil(t, c.Blocks)
	assert.Empty(t, c.Blocks)
	assert.Empty(t, signals.updates, "a read never notifies")
}

func TestCanvasPutBlockValidation(t *testing.T) {
	svc, signals := testCanvasService(t)
	ctx := t.Context()

	cases := map[string]canvas.Block{
		"empty id":                {ID: "  ", Kind: canvas.KindMarkdown, Body: "x"},
		"id too long":             {ID: strings.Repeat("a", 201), Kind: canvas.KindMarkdown, Body: "x"},
		"unknown kind":            {ID: "a", Kind: "diagram", Body: "x"},
		"markdown without body":   {ID: "a", Kind: canvas.KindMarkdown},
		"markdown with url":       {ID: "a", Kind: canvas.KindMarkdown, Body: "x", URL: "https://example.com"},
		"oversized body":          {ID: "a", Kind: canvas.KindMarkdown, Body: strings.Repeat("x", maxCanvasBodyBytes+1)},
		"html without body":       {ID: "a", Kind: canvas.KindHTML},
		"html with url":           {ID: "a", Kind: canvas.KindHTML, Body: "<p>x</p>", URL: "https://example.com"},
		"html with a script":      {ID: "a", Kind: canvas.KindHTML, Body: "<p>x</p><script>alert(1)</script>"},
		"html with a handler":     {ID: "a", Kind: canvas.KindHTML, Body: `<div onclick="x()">x</div>`},
		"html with inline css":    {ID: "a", Kind: canvas.KindHTML, Body: `<div style="color:red">x</div>`},
		"html with an image":      {ID: "a", Kind: canvas.KindHTML, Body: `<img src="https://x.example/a.png">`},
		"html with a stray class": {ID: "a", Kind: canvas.KindHTML, Body: `<div class="hv-card mystery">x</div>`},
		"html with a bad link":    {ID: "a", Kind: canvas.KindHTML, Body: `<a href="javascript:alert(1)">x</a>`},
		"link without title":      {ID: "a", Kind: canvas.KindLink, URL: "https://example.com"},
		"link without url":        {ID: "a", Kind: canvas.KindLink, Title: "t"},
		"link with body":          {ID: "a", Kind: canvas.KindLink, Title: "t", URL: "https://example.com", Body: "x"},
		"javascript url":          {ID: "a", Kind: canvas.KindLink, Title: "t", URL: "javascript:alert(1)"},
	}
	for name, block := range cases {
		_, err := svc.PutBlock(ctx, 1, "plan", "", "", block)
		assert.Equal(t, KindInvalid, KindOf(err), name)
	}

	_, err := svc.PutBlock(ctx, 1, "Bad Name", "", "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	assert.Equal(t, KindInvalid, KindOf(err), "an invalid canvas name is the caller's mistake")
	_, err = svc.PutBlock(ctx, 1, "plan", strings.Repeat("t", maxCanvasTitleLength+1), "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	assert.Equal(t, KindInvalid, KindOf(err), "an oversized canvas title is refused")

	assert.Empty(t, signals.updates, "a refused write never notifies")
}

// A silently stripped tag or class is the one failure an agent cannot see,
// so the error names it and the vocabulary it should have used instead.
func TestCanvasHTMLBlockRejectionNamesTheOffender(t *testing.T) {
	svc, _ := testCanvasService(t)
	ctx := t.Context()

	_, err := svc.PutBlock(ctx, 1, "plan", "", "", canvas.Block{
		ID: "a", Kind: canvas.KindHTML, Body: `<div class="grid-cols-2">x</div>`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"grid-cols-2"`)
	assert.Contains(t, err.Error(), "hv-card", "the message carries the vocabulary the agent should have used")

	_, err = svc.PutBlock(ctx, 1, "plan", "", "", canvas.Block{
		ID: "a", Kind: canvas.KindHTML,
		Body: `<div class="hv-grid hv-cols-2"><span class="hv-badge hv-warn">2 flaky</span></div>`,
	})
	assert.NoError(t, err, "the vocabulary itself is accepted")
}

// The pane's read is the seam between stored agent source and the app's own
// webview; the agent's read-back is not, so the two disagree by design.
func TestCanvasHTMLIsSanitizedOnTheWayOutNotIn(t *testing.T) {
	svc, _ := testCanvasService(t)
	ctx := t.Context()

	// Written straight to the store: validateBlock would have refused this,
	// which is exactly why the read path cannot rely on it.
	_, err := svc.store.Upsert("ws", "plan", 1, "", "", canvas.Block{
		ID: "a", Kind: canvas.KindHTML, Body: `<p class="hv-muted">ok</p><script>alert(1)</script>`,
	})
	require.NoError(t, err)

	shown, err := svc.GetForWorkspace(ctx, "ws", "plan")
	require.NoError(t, err)
	assert.Equal(t, `<p class="hv-muted">ok</p>`, shown.Blocks[0].Body)

	stored, err := svc.Get(ctx, 1, "plan")
	require.NoError(t, err)
	assert.Contains(t, stored.Blocks[0].Body, "<script>", "read_canvas shows the agent what it wrote")
}

func TestCanvasPutBlocks(t *testing.T) {
	svc, signals := testCanvasService(t)
	ctx := t.Context()

	_, err := svc.PutBlocks(ctx, 1, "plan", "The Plan", nil)
	assert.Equal(t, KindInvalid, KindOf(err), "an empty batch is refused")

	_, err = svc.PutBlocks(ctx, 1, "plan", "", []canvas.Block{
		{ID: "a", Kind: canvas.KindMarkdown, Body: "x"},
		{ID: "b", Kind: canvas.KindLink, Title: "t"},
	})
	assert.Equal(t, KindInvalid, KindOf(err))
	assert.Contains(t, err.Error(), "blocks[1]", "the error names which block was rejected")

	_, err = svc.PutBlocks(ctx, 1, "plan", "", []canvas.Block{
		{ID: "a", Kind: canvas.KindMarkdown, Body: "x"},
		{ID: "a", Kind: canvas.KindMarkdown, Body: "y"},
	})
	assert.Equal(t, KindInvalid, KindOf(err), "duplicate ids in one batch are refused")

	metas, err := svc.ListForWorkspace(ctx, "ws")
	require.NoError(t, err)
	assert.Empty(t, metas, "a rejected batch writes nothing")
	assert.Empty(t, signals.updates)

	c, err := svc.PutBlocks(ctx, 1, "plan", "The Plan", []canvas.Block{
		{ID: "a", Kind: canvas.KindMarkdown, Body: "x"},
		{ID: "b", Kind: canvas.KindMarkdown, Body: "y"},
	})
	require.NoError(t, err)
	assert.Len(t, c.Blocks, 2)
	assert.Len(t, signals.updates, 1, "one batch, one notify")

	oversized := make([]canvas.Block, maxCanvasBatchBlocks+1)
	for i := range oversized {
		oversized[i] = canvas.Block{ID: strings.Repeat("a", i+1), Kind: canvas.KindMarkdown, Body: "x"}
	}
	_, err = svc.PutBlocks(ctx, 1, "plan", "", oversized)
	assert.Equal(t, KindInvalid, KindOf(err))
}

func TestCanvasPutBlockBeforeAnchor(t *testing.T) {
	svc, _ := testCanvasService(t)
	ctx := t.Context()

	_, err := svc.PutBlock(ctx, 1, "plan", "", "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	require.NoError(t, err)

	c, err := svc.PutBlock(ctx, 1, "plan", "", "a", canvas.Block{ID: "intro", Kind: canvas.KindMarkdown, Body: "i"})
	require.NoError(t, err)
	assert.Equal(t, "intro", c.Blocks[0].ID)

	_, err = svc.PutBlock(ctx, 1, "plan", "", "ghost", canvas.Block{ID: "x", Kind: canvas.KindMarkdown, Body: "x"})
	assert.Equal(t, KindNotFound, KindOf(err), "a missing anchor is the caller's mistake")
}

func TestCanvasMutationsNotify(t *testing.T) {
	svc, signals := testCanvasService(t)
	ctx := t.Context()

	_, err := svc.PutBlock(ctx, 1, "plan", "The Plan", "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	require.NoError(t, err)
	_, err = svc.PutBlock(ctx, 1, "plan", "", "", canvas.Block{ID: "pr", Kind: canvas.KindLink, Title: "PR", URL: "https://example.com/pr/1"})
	require.NoError(t, err)
	_, err = svc.RemoveBlock(ctx, 1, "plan", "pr")
	require.NoError(t, err)
	c, err := svc.Clear(ctx, 1, "plan")
	require.NoError(t, err)
	assert.Empty(t, c.Blocks)
	assert.Equal(t, "The Plan", c.Title)
	err = svc.Delete(ctx, 1, "plan")
	require.NoError(t, err)

	require.Len(t, signals.updates, 5)
	for _, update := range signals.updates {
		assert.Equal(t, int64(1), update)
	}
}

func TestCanvasMutationsOnUnknownCanvasAreNotFound(t *testing.T) {
	svc, signals := testCanvasService(t)
	ctx := t.Context()

	_, err := svc.RemoveBlock(ctx, 1, "ghost", "a")
	assert.Equal(t, KindNotFound, KindOf(err))
	_, err = svc.Clear(ctx, 1, "ghost")
	assert.Equal(t, KindNotFound, KindOf(err))
	err = svc.Delete(ctx, 1, "ghost")
	assert.Equal(t, KindNotFound, KindOf(err))
	assert.Empty(t, signals.updates)
}

func TestCanvasSetPaneOpen(t *testing.T) {
	svc, signals := testCanvasService(t)
	ctx := t.Context()

	require.NoError(t, svc.SetPaneOpen(ctx, 1, "", true), "opening without a name leaves the pane's own pick")
	err := svc.SetPaneOpen(ctx, 1, "ghost", true)
	assert.Equal(t, KindNotFound, KindOf(err), "opening pinned to a canvas requires it to exist")

	_, err = svc.PutBlock(ctx, 1, "plan", "", "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	require.NoError(t, err)
	require.NoError(t, svc.SetPaneOpen(ctx, 1, "plan", true))
	require.NoError(t, svc.SetPaneOpen(ctx, 1, "", false))
	err = svc.SetPaneOpen(ctx, 99, "", true)
	assert.Equal(t, KindNotFound, KindOf(err))

	assert.Equal(t, []canvasToggle{
		{1, "", true},
		{1, "plan", true},
		{1, "", false},
	}, signals.toggles)
	assert.Len(t, signals.updates, 1, "a pane toggle is not a content update")
}

func TestCanvasRemoveAbsentBlockIsNotFound(t *testing.T) {
	svc, _ := testCanvasService(t)
	ctx := t.Context()
	_, err := svc.PutBlock(ctx, 1, "plan", "", "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	require.NoError(t, err)
	_, err = svc.RemoveBlock(ctx, 1, "plan", "ghost")
	assert.Equal(t, KindNotFound, KindOf(err))
}

func TestCanvasExport(t *testing.T) {
	svc, signals := testCanvasService(t)
	ctx := t.Context()

	_, err := svc.MarkdownForWorkspace(ctx, "ws", "ghost")
	assert.Equal(t, KindNotFound, KindOf(err))

	_, err = svc.PutBlock(ctx, 1, "plan", "The Plan", "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "hello"})
	require.NoError(t, err)

	markdown, err := svc.MarkdownForWorkspace(ctx, "ws", "plan")
	require.NoError(t, err)
	assert.Equal(t, "# The Plan\n\nhello\n", markdown)

	err = svc.ExportForWorkspace(ctx, "ws", "plan", "relative.md")
	assert.Equal(t, KindInvalid, KindOf(err), "the save dialog hands back absolute paths; anything else is a caller bug")

	dest := filepath.Join(t.TempDir(), "plan.md")
	require.NoError(t, svc.ExportForWorkspace(ctx, "ws", "plan", dest))
	written, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, markdown, string(written))

	assert.Len(t, signals.updates, 1, "an export is not a content update")
}

func TestCanvasListForWorkspace(t *testing.T) {
	svc, _ := testCanvasService(t)
	ctx := t.Context()

	metas, err := svc.ListForWorkspace(ctx, "ws")
	require.NoError(t, err)
	assert.Empty(t, metas)

	_, err = svc.PutBlock(ctx, 1, "plan", "The Plan", "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	require.NoError(t, err)
	metas, err = svc.ListForWorkspace(ctx, "ws")
	require.NoError(t, err)
	require.Len(t, metas, 1)
	assert.Equal(t, "plan", metas[0].Name)
	assert.Equal(t, "The Plan", metas[0].Title)
	assert.Equal(t, int64(1), metas[0].Session)

	fromSession, err := svc.List(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, metas, fromSession, "List resolves the session's workspace and answers the same rows")

	_, err = svc.ListForWorkspace(ctx, "../escape")
	assert.Equal(t, KindInvalid, KindOf(err))
}
