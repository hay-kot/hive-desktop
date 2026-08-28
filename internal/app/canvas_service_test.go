package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hay-kot/hive-desktop/internal/app/canvas"
	"github.com/hay-kot/hive-desktop/internal/app/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCanvasSessions map[int64]store.AgentWorkspaceSession

func (f fakeCanvasSessions) GetAgentWorkspaceSession(_ context.Context, id int64) (store.AgentWorkspaceSession, bool, error) {
	rec, ok := f[id]
	return rec, ok, nil
}

type canvasUpdate struct {
	workspace string
	session   int64
}

type canvasToggle struct {
	workspace string
	session   int64
	name      string
	open      bool
}

type canvasSignals struct {
	updates []canvasUpdate
	toggles []canvasToggle
}

func testCanvasService(t *testing.T) (*CanvasService, *canvasSignals) {
	t.Helper()
	signals := &canvasSignals{}
	sessions := fakeCanvasSessions{
		1: {ID: 1, Workspace: "ws", Name: "chat", Agent: "claude"},
	}
	svc := newCanvasService(canvas.NewStore(t.TempDir()), sessions,
		func(workspace string, session int64) {
			signals.updates = append(signals.updates, canvasUpdate{workspace, session})
		},
		func(workspace string, session int64, name string, open bool) {
			signals.toggles = append(signals.toggles, canvasToggle{workspace, session, name, open})
		})
	return svc, signals
}

func TestCanvasUnknownSessionIsNotFound(t *testing.T) {
	svc, _ := testCanvasService(t)
	ctx := t.Context()

	_, err := svc.Get(ctx, 99, "plan")
	assert.Equal(t, KindNotFound, KindOf(err))
	_, err = svc.PutBlock(ctx, 99, "plan", "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
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
		"empty id":              {ID: "  ", Kind: canvas.KindMarkdown, Body: "x"},
		"id too long":           {ID: strings.Repeat("a", 201), Kind: canvas.KindMarkdown, Body: "x"},
		"unknown kind":          {ID: "a", Kind: "html", Body: "x"},
		"markdown without body": {ID: "a", Kind: canvas.KindMarkdown},
		"markdown with url":     {ID: "a", Kind: canvas.KindMarkdown, Body: "x", URL: "https://example.com"},
		"oversized body":        {ID: "a", Kind: canvas.KindMarkdown, Body: strings.Repeat("x", maxCanvasBodyBytes+1)},
		"link without title":    {ID: "a", Kind: canvas.KindLink, URL: "https://example.com"},
		"link without url":      {ID: "a", Kind: canvas.KindLink, Title: "t"},
		"link with body":        {ID: "a", Kind: canvas.KindLink, Title: "t", URL: "https://example.com", Body: "x"},
		"javascript url":        {ID: "a", Kind: canvas.KindLink, Title: "t", URL: "javascript:alert(1)"},
	}
	for name, block := range cases {
		_, err := svc.PutBlock(ctx, 1, "plan", "", block)
		assert.Equal(t, KindInvalid, KindOf(err), name)
	}

	_, err := svc.PutBlock(ctx, 1, "Bad Name", "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	assert.Equal(t, KindInvalid, KindOf(err), "an invalid canvas name is the caller's mistake")
	_, err = svc.PutBlock(ctx, 1, "plan", strings.Repeat("t", maxCanvasTitleLength+1), canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	assert.Equal(t, KindInvalid, KindOf(err), "an oversized canvas title is refused")

	assert.Empty(t, signals.updates, "a refused write never notifies")
}

func TestCanvasMutationsNotify(t *testing.T) {
	svc, signals := testCanvasService(t)
	ctx := t.Context()

	_, err := svc.PutBlock(ctx, 1, "plan", "The Plan", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	require.NoError(t, err)
	_, err = svc.PutBlock(ctx, 1, "plan", "", canvas.Block{ID: "pr", Kind: canvas.KindLink, Title: "PR", URL: "https://example.com/pr/1"})
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
		assert.Equal(t, canvasUpdate{"ws", 1}, update)
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

	_, err = svc.PutBlock(ctx, 1, "plan", "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	require.NoError(t, err)
	require.NoError(t, svc.SetPaneOpen(ctx, 1, "plan", true))
	require.NoError(t, svc.SetPaneOpen(ctx, 1, "", false))
	err = svc.SetPaneOpen(ctx, 99, "", true)
	assert.Equal(t, KindNotFound, KindOf(err))

	assert.Equal(t, []canvasToggle{
		{"ws", 1, "", true},
		{"ws", 1, "plan", true},
		{"ws", 1, "", false},
	}, signals.toggles)
	assert.Len(t, signals.updates, 1, "a pane toggle is not a content update")
}

func TestCanvasRemoveAbsentBlockIsNotFound(t *testing.T) {
	svc, _ := testCanvasService(t)
	ctx := t.Context()
	_, err := svc.PutBlock(ctx, 1, "plan", "", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	require.NoError(t, err)
	_, err = svc.RemoveBlock(ctx, 1, "plan", "ghost")
	assert.Equal(t, KindNotFound, KindOf(err))
}

func TestCanvasExport(t *testing.T) {
	svc, signals := testCanvasService(t)
	ctx := t.Context()

	_, err := svc.MarkdownForWorkspace(ctx, "ws", "ghost")
	assert.Equal(t, KindNotFound, KindOf(err))

	_, err = svc.PutBlock(ctx, 1, "plan", "The Plan", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "hello"})
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

	_, err = svc.PutBlock(ctx, 1, "plan", "The Plan", canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
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
