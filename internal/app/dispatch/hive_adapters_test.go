package dispatch

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/colonyops/hive/pkg/executil"
	"github.com/colonyops/hive/pkg/tmpl"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/config"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/eventbus"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/git"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/messaging"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/session"
	coredb "github.com/hay-kot/hive-desktop/internal/hivecore/data/db"
	"github.com/hay-kot/hive-desktop/internal/hivecore/data/stores"
	hivesvc "github.com/hay-kot/hive-desktop/internal/hivecore/hive"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type durableMessageServiceTest struct {
	message messaging.Message
	topics  []string
}

func (s *durableMessageServiceTest) Publish(_ context.Context, message messaging.Message, topics []string) (messaging.PublishResult, error) {
	s.message, s.topics = message, topics
	return messaging.PublishResult{Topics: topics}, nil
}

func TestHiveMessagePublisherUsesFixedSenderEmptySessionAndLiteralTopic(t *testing.T) {
	service := &durableMessageServiceTest{}
	topic, err := NewHiveMessagePublisher(service).PublishMessage(t.Context(), "hello", "agent.session.inbox")
	require.NoError(t, err)
	assert.Equal(t, "agent.session.inbox", topic)
	assert.Equal(t, messaging.Message{Payload: "hello", Sender: "hive-desktop", SessionID: ""}, service.message)
	assert.Equal(t, []string{"agent.session.inbox"}, service.topics)
}

func TestHiveMessagePublisherPersistsThroughCoreSQLiteReopen(t *testing.T) {
	dir := t.TempDir()
	first, err := coredb.Open(dir, coredb.DefaultOpenOptions())
	require.NoError(t, err)
	service := hivesvc.NewMessageService(stores.NewMessageStore(first, 0), &config.Config{}, eventbus.New(8))
	publisher := NewHiveMessagePublisher(service)
	const topic = "agent.session.inbox"
	published, err := publisher.PublishMessage(t.Context(), "hello from desktop", topic)
	require.NoError(t, err)
	assert.Equal(t, topic, published)
	require.NoError(t, first.Close())

	reopened, err := coredb.Open(dir, coredb.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	reopenedService := hivesvc.NewMessageService(stores.NewMessageStore(reopened, 0), &config.Config{}, eventbus.New(8))
	messages, err := reopenedService.Subscribe(t.Context(), topic, time.Time{})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "hello from desktop", messages[0].Payload)
	assert.Equal(t, "hive-desktop", messages[0].Sender)
	assert.Empty(t, messages[0].SessionID)
	assert.Equal(t, topic, messages[0].Topic)
}

// newHiveSessions builds the real vendored session service over a temporary
// core database. It is what makes these tests worth having: the seam's job is
// to hold against hive's actual implementation, so SessionManagement is
// satisfied structurally here rather than by a fake shaped to fit it.
func newHiveSessions(t *testing.T) (*HiveSessionManager, session.Store) {
	t.Helper()
	database, err := coredb.Open(t.TempDir(), coredb.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })

	store := stores.NewSessionStore(database)
	svc := hivesvc.NewSessionService(
		store,
		git.NewExecutor("git", &executil.RealExecutor{}),
		&config.Config{},
		eventbus.New(8),
		&executil.RealExecutor{},
		tmpl.New(tmpl.Config{}),
		zerolog.Nop(),
		io.Discard,
		io.Discard,
	)
	return NewHiveSessionManager(svc), store
}

func TestHiveSessionManagerListsEveryState(t *testing.T) {
	manager, store := newHiveSessions(t)
	now := time.Now().UTC().Truncate(time.Second)

	active := session.Session{ID: "s1", Name: "review 81", Slug: "review-81", Path: "/tmp/review-81", Remote: "acme/site", State: session.StateActive, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.Save(t.Context(), active))
	require.NoError(t, store.Save(t.Context(), session.Session{ID: "s2", Name: "old", Slug: "old", Path: "/tmp/old", State: session.StateRecycled, CreatedAt: now, UpdatedAt: now}))

	got, err := manager.ListSessions(t.Context())
	require.NoError(t, err)
	assert.ElementsMatch(t, []SessionSummary{
		{ID: "s1", Name: "review 81", Slug: "review-81", Repo: "acme/site", State: "active"},
		{ID: "s2", Name: "old", Slug: "old", State: "recycled"},
	}, got)
}

func TestHiveSessionManagerDetailReadsWorktreeMetadata(t *testing.T) {
	manager, store := newHiveSessions(t)
	now := time.Now().UTC().Truncate(time.Second)

	sess := session.Session{
		ID: "s1", Name: "review 81", Slug: "review-81", Path: "/tmp/review-81",
		Remote: "acme/site", State: session.StateActive, CloneStrategy: session.CloneStrategyWorktree,
		Tags: []string{"pr-81"}, CreatedAt: now, UpdatedAt: now,
	}
	sess.SetMeta(session.MetaWorktreeBranch, "hive/review-81")
	require.NoError(t, store.Save(t.Context(), sess))

	detail, err := manager.SessionDetail(t.Context(), "s1")
	require.NoError(t, err)
	// Timestamps come back in the local zone, so they are compared by instant.
	assert.True(t, detail.CreatedAt.Equal(now))
	assert.True(t, detail.UpdatedAt.Equal(now))
	detail.CreatedAt, detail.UpdatedAt = now, now
	assert.Equal(t, SessionDetail{
		ID: "s1", Name: "review 81", Slug: "review-81", Repo: "acme/site", State: "active",
		Path: "/tmp/review-81", CloneStrategy: session.CloneStrategyWorktree,
		WorktreeBranch: "hive/review-81", Tags: []string{"pr-81"}, CreatedAt: now, UpdatedAt: now,
	}, detail)

	_, err = manager.SessionDetail(t.Context(), "missing")
	require.Error(t, err)
}

func TestHiveSessionManagerRenameReSlugsTheSession(t *testing.T) {
	manager, store := newHiveSessions(t)
	now := time.Now().UTC()
	require.NoError(t, store.Save(t.Context(), session.Session{ID: "s1", Name: "review 81", Slug: "review-81", Path: "/tmp/review-81", State: session.StateActive, CreatedAt: now, UpdatedAt: now}))

	require.NoError(t, manager.RenameSession(t.Context(), "s1", "Review 82"))

	detail, err := manager.SessionDetail(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "Review 82", detail.Name)
	// The slug moving is the hazard the desktop compensates for: it is the tmux
	// session name, and hive renames the record without renaming tmux. See
	// app.SessionsService.RenameSession and ADR 0038.
	assert.Equal(t, "review-82", detail.Slug)
	assert.Equal(t, "/tmp/review-81", detail.Path, "the directory keeps the slug it was cloned under")
}

func TestHiveSessionManagerRiskIsEmptyForANonActiveSession(t *testing.T) {
	manager, store := newHiveSessions(t)
	now := time.Now().UTC()
	require.NoError(t, store.Save(t.Context(), session.Session{ID: "s1", Name: "old", Slug: "old", Path: "/tmp/old", State: session.StateRecycled, CloneStrategy: session.CloneStrategyWorktree, CreatedAt: now, UpdatedAt: now}))

	risk, err := manager.SessionRisk(t.Context(), "s1")
	require.NoError(t, err)
	// No live clone left to hold unsaved work — but recycling a worktree
	// session still deletes it, which the confirmation has to say.
	assert.Equal(t, SessionRisk{RecycleDeletes: true}, risk)
}
