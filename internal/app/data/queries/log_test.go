package queries

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
)

func TestAppend_ReadFrom_Monotonic(t *testing.T) {
	database := openTestDB(t)
	ctx := t.Context()

	var offsets []int64
	for i := range 3 {
		offset, err := database.Append(ctx, "source:test", fmt.Sprintf("key-%d", i), fmt.Appendf(nil, `{"n":%d}`, i))
		require.NoError(t, err)
		offsets = append(offsets, offset)
	}

	// Offsets are strictly increasing.
	for i := 1; i < len(offsets); i++ {
		assert.Greater(t, offsets[i], offsets[i-1])
	}

	msgs, next, err := database.ReadFrom(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 3)
	assert.Equal(t, offsets[2], next)

	for i, msg := range msgs {
		assert.Equal(t, "source:test", msg.Topic)
		assert.Equal(t, fmt.Sprintf("key-%d", i), msg.Key)
		assert.Equal(t, fmt.Sprintf(`{"n":%d}`, i), string(msg.Payload))
		assert.Positive(t, msg.Ts)
		assert.Equal(t, fmt.Sprintf("%d", offsets[i]), msg.ID)
	}

	// Reading from the last offset returns nothing new and leaves nextOffset unchanged.
	msgs, next, err = database.ReadFrom(ctx, offsets[2], 10)
	require.NoError(t, err)
	assert.Empty(t, msgs)
	assert.Equal(t, offsets[2], next)

	// Paged reads resume correctly.
	msgs, next, err = database.ReadFrom(ctx, offsets[0], 1)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "key-1", msgs[0].Key)
	assert.Equal(t, offsets[1], next)
}

func TestReadForConsumer_ResumesFromPersistedOffset(t *testing.T) {
	database := openTestDB(t)
	ctx := t.Context()

	for i := range 3 {
		_, err := database.Append(ctx, "source:test", fmt.Sprintf("key-%d", i), []byte(`{}`))
		require.NoError(t, err)
	}
	require.NoError(t, database.CommitBatch(ctx, models.CommitBatch{Consumer: "flow-1", UpToOffset: 2}))

	msgs, err := database.ReadForConsumer(ctx, "flow-1", 500)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "3", msgs[0].ID)
}

func TestReadFrom_EmptySnapshotSurvivesJSON(t *testing.T) {
	database := openTestDB(t)
	ctx := t.Context()

	_, err := database.AppendSnapshot(ctx, "source:test", "github", "", []models.SnapshotItem{})
	require.NoError(t, err)

	msgs, _, err := database.ReadFrom(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.NotNil(t, msgs[0].Snapshot, "an empty snapshot must decode as a non-nil slice")

	// The frontend engine routes on `msg.Snapshot != null`; an empty snapshot
	// must therefore cross the binding as [] rather than being omitted, or the
	// boundary row (key "") is treated as an ordinary item and models.CommitBatch
	// wedges the consumer on resolving inbox item "<kind>//".
	wire, err := json.Marshal(msgs[0])
	require.NoError(t, err)
	assert.Contains(t, string(wire), `"Snapshot":[]`)
}
