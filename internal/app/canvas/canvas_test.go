package canvas

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testStore returns a store over a temp root whose clock ticks one
// millisecond per call, so every mutation gets a distinct, ordered stamp.
func testStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(t.TempDir())
	var tick int64
	s.now = func() time.Time {
		tick++
		return time.UnixMilli(tick)
	}
	return s
}

func TestLoadAbsentIsNotAnError(t *testing.T) {
	s := testStore(t)
	_, ok, err := s.Load("ws", 1)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestUpsertAppendsThenUpdatesInPlace(t *testing.T) {
	s := testStore(t)

	c, err := s.Upsert("ws", 1, Block{ID: "plan", Kind: KindMarkdown, Body: "v1"})
	require.NoError(t, err)
	require.Len(t, c.Blocks, 1)
	created := c.Blocks[0].CreatedAt
	assert.NotZero(t, created)
	assert.NotZero(t, c.CreatedAt)

	c, err = s.Upsert("ws", 1, Block{ID: "result", Kind: KindLink, Title: "PR", URL: "https://example.com"})
	require.NoError(t, err)
	require.Len(t, c.Blocks, 2)

	c, err = s.Upsert("ws", 1, Block{ID: "plan", Kind: KindMarkdown, Body: "v2"})
	require.NoError(t, err)
	require.Len(t, c.Blocks, 2)
	assert.Equal(t, "plan", c.Blocks[0].ID, "an updated block keeps its position")
	assert.Equal(t, "v2", c.Blocks[0].Body)
	assert.Equal(t, created, c.Blocks[0].CreatedAt, "an updated block keeps its CreatedAt")
	assert.Greater(t, c.Blocks[0].UpdatedAt, created)

	reloaded, ok, err := s.Load("ws", 1)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, c, reloaded)
}

func TestRemove(t *testing.T) {
	s := testStore(t)
	_, err := s.Upsert("ws", 1, Block{ID: "a", Kind: KindMarkdown, Body: "x"})
	require.NoError(t, err)

	c, removed, err := s.Remove("ws", 1, "a")
	require.NoError(t, err)
	assert.True(t, removed)
	assert.Empty(t, c.Blocks)

	_, removed, err = s.Remove("ws", 1, "a")
	require.NoError(t, err)
	assert.False(t, removed)

	_, removed, err = s.Remove("ws", 99, "a")
	require.NoError(t, err)
	assert.False(t, removed, "a never-written canvas removes nothing")
}

func TestClearKeepsTheCanvas(t *testing.T) {
	s := testStore(t)
	first, err := s.Upsert("ws", 1, Block{ID: "a", Kind: KindMarkdown, Body: "x"})
	require.NoError(t, err)

	c, err := s.Clear("ws", 1)
	require.NoError(t, err)
	assert.Empty(t, c.Blocks)
	assert.Equal(t, first.CreatedAt, c.CreatedAt, "clear keeps the canvas's CreatedAt")

	reloaded, ok, err := s.Load("ws", 1)
	require.NoError(t, err)
	require.True(t, ok, "the file survives a clear")
	assert.Empty(t, reloaded.Blocks)
}

func TestListOrdersByUpdatedAtDesc(t *testing.T) {
	s := testStore(t)
	_, err := s.Upsert("ws", 1, Block{ID: "a", Kind: KindMarkdown, Body: "x"})
	require.NoError(t, err)
	_, err = s.Upsert("ws", 2, Block{ID: "a", Kind: KindMarkdown, Body: "x"})
	require.NoError(t, err)
	_, err = s.Upsert("ws", 1, Block{ID: "b", Kind: KindMarkdown, Body: "y"})
	require.NoError(t, err)

	metas, err := s.List("ws")
	require.NoError(t, err)
	require.Len(t, metas, 2)
	assert.Equal(t, int64(1), metas[0].Session, "the most recently updated canvas lists first")
	assert.Equal(t, 2, metas[0].BlockCount)

	empty, err := s.List("other")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestDeleteSessionAndWorkspaceAreIdempotent(t *testing.T) {
	s := testStore(t)
	_, err := s.Upsert("ws", 1, Block{ID: "a", Kind: KindMarkdown, Body: "x"})
	require.NoError(t, err)

	require.NoError(t, s.DeleteSession("ws", 1))
	require.NoError(t, s.DeleteSession("ws", 1))
	_, ok, err := s.Load("ws", 1)
	require.NoError(t, err)
	assert.False(t, ok)

	_, err = s.Upsert("ws", 2, Block{ID: "a", Kind: KindMarkdown, Body: "x"})
	require.NoError(t, err)
	require.NoError(t, s.DeleteWorkspace("ws"))
	require.NoError(t, s.DeleteWorkspace("ws"))
	metas, err := s.List("ws")
	require.NoError(t, err)
	assert.Empty(t, metas)
}

func TestInvalidWorkspaceRefused(t *testing.T) {
	s := testStore(t)
	for _, dir := range []string{"", ".", "..", "a/b", "../escape"} {
		_, err := s.Upsert(dir, 1, Block{ID: "a", Kind: KindMarkdown, Body: "x"})
		require.ErrorIs(t, err, ErrInvalidWorkspace, "dir %q", dir)
		_, err = s.List(dir)
		require.ErrorIs(t, err, ErrInvalidWorkspace, "dir %q", dir)
	}
}

func TestCorruptFileIsAnErrorNotABlankCanvas(t *testing.T) {
	s := testStore(t)
	_, err := s.Upsert("ws", 1, Block{ID: "a", Kind: KindMarkdown, Body: "x"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(s.root, "ws", "1.json"), []byte("{not json"), 0o600))

	_, _, err = s.Load("ws", 1)
	require.Error(t, err)
	_, err = s.List("ws")
	require.Error(t, err)
}

func TestWritesLeaveNoTempFiles(t *testing.T) {
	s := testStore(t)
	_, err := s.Upsert("ws", 1, Block{ID: "a", Kind: KindMarkdown, Body: "x"})
	require.NoError(t, err)
	_, err = s.Clear("ws", 1)
	require.NoError(t, err)

	entries, err := os.ReadDir(filepath.Join(s.root, "ws"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "1.json", entries[0].Name())

	metas, err := s.List("ws")
	require.NoError(t, err)
	assert.Len(t, metas, 1, "a stray non-canvas file must not break the listing")
}
