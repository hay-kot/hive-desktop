package stores

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

func TestSeed_WritesExactFieldsNoStoreMethodAllows(t *testing.T) {
	_, db := openTestStores(t)
	ctx := t.Context()
	seed := NewSeed(db)

	item, err := seed.InboxItem(ctx, queries.InsertInboxItemParams{
		ProfileID: "p", SourceKind: "github", SourceScope: "s", ExternalID: "seeded-1",
		Payload: []byte(`{}`), Lifecycle: "active", FirstSeenAt: 1_700_000_000_000, LastEventAt: 1_700_000_001_000,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1_700_000_000_000), item.FirstSeenAt, "the fixture's exact timestamp survives, not the store's own clock")

	event, err := seed.InboxEvent(ctx, queries.InsertInboxEventParams{
		ItemID: item.ID, Kind: "observed", Transition: "none", Attention: "trivial", Detail: []byte(`{}`), CreatedAt: 1_700_000_002_000,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1_700_000_002_000), event.CreatedAt)

	require.NoError(t, seed.ConsumerOffset(ctx, "consumer-a", 5))

	job, err := seed.Job(ctx, queries.InsertJobParams{CreatedAt: 100, UpdatedAt: 900, Status: "done", Label: "backfilled"})
	require.NoError(t, err)
	assert.Equal(t, int64(100), job.CreatedAt)
	assert.Equal(t, int64(900), job.UpdatedAt, "CreatedAt and UpdatedAt can differ, unlike JobStore.Insert which stamps both from one clock read")
}
