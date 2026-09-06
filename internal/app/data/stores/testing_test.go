package stores

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// openTestStores opens a fresh SQLite database in a temp directory and
// builds every store over it, the fixture every store test in this package
// shares.
func openTestStores(t *testing.T) (*Stores, *queries.DB) {
	t.Helper()
	db, err := queries.Open(t.Context(), t.TempDir(), queries.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return New(db, Options{}), db
}

// testClassifier and activityClassifier mirror the queries package's own
// IngestObservation test fixtures (data/queries/inbox_item_test.go), needed
// here because setting up an ItemSessionStore or InboxItemStore fixture
// through the production boundary still means calling *queries.DB.
type testClassifier struct {
	classify func(*models.Observation, models.Observation) models.Classification
}

func (c testClassifier) Classify(prev *models.Observation, current models.Observation) models.Classification {
	return c.classify(prev, current)
}

func activityClassifier(key string) testClassifier {
	return testClassifier{func(_ *models.Observation, _ models.Observation) models.Classification {
		return models.Classification{Kind: "activity", Attention: models.AttentionActivity, Transition: models.TransitionNone, Lifecycle: models.LifecycleActive, OccurrenceKey: key, Summary: "activity"}
	}}
}

// testClock is a hand-advanced clock for tests whose assertions depend on
// controlled timestamps -- Options.Now is the seam that replaces what
// jobs.Options.Now and activity.Options.Now gave the old packages for the
// same reason.
type testClock struct{ now time.Time }

func newTestClock() *testClock { return &testClock{now: time.UnixMilli(1)} }

func (c *testClock) Now() time.Time { return c.now }

func (c *testClock) set(unixMilli int64) { c.now = time.UnixMilli(unixMilli) }
