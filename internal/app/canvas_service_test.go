package app

import (
	"context"
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

func testCanvasService(t *testing.T) (*CanvasService, *[]canvasUpdate) {
	t.Helper()
	updates := &[]canvasUpdate{}
	sessions := fakeCanvasSessions{
		1: {ID: 1, Workspace: "ws", Name: "chat", Agent: "claude"},
	}
	svc := newCanvasService(canvas.NewStore(t.TempDir()), sessions, func(workspace string, session int64) {
		*updates = append(*updates, canvasUpdate{workspace, session})
	})
	return svc, updates
}

func TestCanvasUnknownSessionIsNotFound(t *testing.T) {
	svc, _ := testCanvasService(t)
	ctx := t.Context()

	_, err := svc.Get(ctx, 99)
	assert.Equal(t, KindNotFound, KindOf(err))
	_, err = svc.PutBlock(ctx, 99, canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	assert.Equal(t, KindNotFound, KindOf(err))
	_, err = svc.RemoveBlock(ctx, 99, "a")
	assert.Equal(t, KindNotFound, KindOf(err))
	_, err = svc.Clear(ctx, 99)
	assert.Equal(t, KindNotFound, KindOf(err))
}

func TestCanvasGetOnUnwrittenSessionAnswersEmpty(t *testing.T) {
	svc, updates := testCanvasService(t)

	c, err := svc.Get(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, "ws", c.Workspace, "the workspace comes from the session record")
	assert.NotNil(t, c.Blocks)
	assert.Empty(t, c.Blocks)
	assert.Empty(t, *updates, "a read never notifies")
}

func TestCanvasPutBlockValidation(t *testing.T) {
	svc, updates := testCanvasService(t)
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
		_, err := svc.PutBlock(ctx, 1, block)
		assert.Equal(t, KindInvalid, KindOf(err), name)
	}
	assert.Empty(t, *updates, "a refused write never notifies")
}

func TestCanvasMutationsNotify(t *testing.T) {
	svc, updates := testCanvasService(t)
	ctx := t.Context()

	_, err := svc.PutBlock(ctx, 1, canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	require.NoError(t, err)
	_, err = svc.PutBlock(ctx, 1, canvas.Block{ID: "pr", Kind: canvas.KindLink, Title: "PR", URL: "https://example.com/pr/1"})
	require.NoError(t, err)
	_, err = svc.RemoveBlock(ctx, 1, "pr")
	require.NoError(t, err)
	c, err := svc.Clear(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, c.Blocks)

	require.Len(t, *updates, 4)
	for _, update := range *updates {
		assert.Equal(t, canvasUpdate{"ws", 1}, update)
	}
}

func TestCanvasRemoveAbsentBlockIsNotFound(t *testing.T) {
	svc, _ := testCanvasService(t)
	_, err := svc.RemoveBlock(t.Context(), 1, "ghost")
	assert.Equal(t, KindNotFound, KindOf(err))
}

func TestCanvasListForWorkspace(t *testing.T) {
	svc, _ := testCanvasService(t)
	ctx := t.Context()

	metas, err := svc.ListForWorkspace(ctx, "ws")
	require.NoError(t, err)
	assert.Empty(t, metas)

	_, err = svc.PutBlock(ctx, 1, canvas.Block{ID: "a", Kind: canvas.KindMarkdown, Body: "x"})
	require.NoError(t, err)
	metas, err = svc.ListForWorkspace(ctx, "ws")
	require.NoError(t, err)
	require.Len(t, metas, 1)
	assert.Equal(t, int64(1), metas[0].Session)

	_, err = svc.ListForWorkspace(ctx, "../escape")
	assert.Equal(t, KindInvalid, KindOf(err))
}
