package stores

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivityEventStore_AppendAndList(t *testing.T) {
	st, _ := openTestStores(t)
	ctx := t.Context()

	first, err := st.ActivityEvents.Append(ctx, ActivityEventCreate{
		Category: "action", Severity: "info", Title: "first", Source: "test",
	})
	require.NoError(t, err)
	assert.NotZero(t, first.ID)
	assert.NotZero(t, first.CreatedAt)

	second, err := st.ActivityEvents.Append(ctx, ActivityEventCreate{
		Category: "action", Severity: "warn", Title: "second", Source: "test", Metadata: []byte(`{"k":"v"}`),
	})
	require.NoError(t, err)

	events, err := st.ActivityEvents.List(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, second.ID, events[0].ID, "newest first")
	assert.Equal(t, first.ID, events[1].ID)
	assert.JSONEq(t, `{"k":"v"}`, string(events[0].Metadata))

	// Paging with a before cursor returns only the older row.
	older, err := st.ActivityEvents.List(ctx, second.ID, 10)
	require.NoError(t, err)
	require.Len(t, older, 1)
	assert.Equal(t, first.ID, older[0].ID)
}
