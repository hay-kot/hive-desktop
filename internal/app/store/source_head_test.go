package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ingestActiveSourceHead(t *testing.T, db *DB, topic, profileID, sourceScope, externalID string) {
	t.Helper()
	_, err := db.IngestObservation(t.Context(), activityClassifier(externalID), IngestObservationParams{
		ProfileID: profileID,
		Topic:     topic,
		Current: Observation{
			ExternalID: externalID, Title: externalID, SourceKind: "github", SourceScope: sourceScope,
			ObservedAt: 1, Payload: []byte(`{"v":1}`),
		},
	})
	require.NoError(t, err)
}

func TestListActiveSourceHeadKeys(t *testing.T) {
	db := openTestDB(t)
	ctx := t.Context()
	const topic = "source:profile/source"

	ingestActiveSourceHead(t, db, topic, "profile", "main", "active-item")

	ingestActiveSourceHead(t, db, topic, "profile", "main", "archived-item")
	_, err := db.Conn().ExecContext(ctx, `UPDATE inbox_item SET archived_at = 1, archived_actor = 'manual' WHERE external_id = ?`, "archived-item")
	require.NoError(t, err)

	ingestActiveSourceHead(t, db, topic, "profile", "main", "orphaned-item")
	_, err = db.Conn().ExecContext(ctx, `DELETE FROM inbox_item WHERE external_id = ?`, "orphaned-item")
	require.NoError(t, err)

	ingestActiveSourceHead(t, db, topic, "profile", "other-scope", "wrong-scope-item")

	keys, err := db.ListActiveSourceHeadKeys(ctx, SourceIdentity{
		Topic: topic, ProfileID: "profile", SourceKind: "github", SourceScope: "main",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"active-item"}, keys)
}
