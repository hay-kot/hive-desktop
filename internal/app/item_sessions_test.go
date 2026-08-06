package app

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/store"
)

// fakeItemSessionStore stands in for the durable link table. links is keyed by
// external id, which is enough to tell one item's sessions from another's here.
type fakeItemSessionStore struct {
	refs      map[int64]store.ItemRef
	links     map[string][]store.ItemSession
	unlinked  []string
	unlinkErr error
}

func (f *fakeItemSessionStore) ItemRefByID(_ context.Context, itemID int64) (store.ItemRef, error) {
	ref, ok := f.refs[itemID]
	if !ok {
		return store.ItemRef{}, sql.ErrNoRows
	}
	return ref, nil
}

func (f *fakeItemSessionStore) ItemSessions(_ context.Context, ref store.ItemRef) ([]store.ItemSession, error) {
	return f.links[ref.ExternalID], nil
}

func (f *fakeItemSessionStore) UnlinkItemSessions(_ context.Context, sessionIDs []string) error {
	if f.unlinkErr != nil {
		return f.unlinkErr
	}
	f.unlinked = append(f.unlinked, sessionIDs...)
	return nil
}

func itemSessionsService(manager *fakeSessionManager, links *fakeItemSessionStore) *SessionsService {
	return &sessionsDeps{
		launcher: &fakeSessionLauncher{}, manager: manager, statuses: manager,
		tmux: &fakeSessionTmux{}, jobs: &fakeJobRunner{}, links: links, logger: zerolog.Nop(),
	}
}

func TestSessionsService_ItemSessionsJoinsLinksToLiveHiveState(t *testing.T) {
	manager := &fakeSessionManager{
		sessions: []dispatch.SessionSummary{
			{ID: "s1", Name: "review 81 renamed", Slug: "review-81-renamed", Repo: "acme/site", State: "active"},
			{ID: "s2", Name: "review 81 rerun", Slug: "review-81-rerun", Repo: "acme/site", State: "recycled"},
		},
		running: map[string]bool{"s1": true},
	}
	links := &fakeItemSessionStore{
		refs: map[int64]store.ItemRef{7: {ProfileID: "p", SourceKind: "github", ExternalID: "acme/site#81"}},
		links: map[string][]store.ItemSession{"acme/site#81": {
			{SessionID: "s2", CreatedAt: 200},
			{SessionID: "s1", CreatedAt: 100},
		}},
	}

	views, err := itemSessionsService(manager, links).ItemSessions(t.Context(), 7)
	require.NoError(t, err)
	require.Len(t, views, 2)

	// The link order is preserved, and everything but createdAt comes from
	// hive — so a session renamed outside this app reports its current name.
	assert.Equal(t, "s2", views[0].ID)
	assert.Equal(t, "recycled", views[0].State)
	assert.False(t, views[0].Running)
	assert.Equal(t, "s1", views[1].ID)
	assert.Equal(t, "review 81 renamed", views[1].Name)
	assert.Equal(t, "review-81-renamed", views[1].Slug)
	assert.True(t, views[1].Running)
	assert.Equal(t, int64(100), views[1].CreatedAt.UnixMilli())
}

// Nothing tells this app when a session is deleted from the CLI, so the read
// is what notices — and it must both hide and drop the link.
func TestSessionsService_ItemSessionsPrunesLinksHiveCannotAccountFor(t *testing.T) {
	manager := &fakeSessionManager{
		sessions: []dispatch.SessionSummary{{ID: "s1", Name: "kept", Slug: "kept", State: "active"}},
	}
	links := &fakeItemSessionStore{
		refs: map[int64]store.ItemRef{7: {ProfileID: "p", ExternalID: "acme/site#81"}},
		links: map[string][]store.ItemSession{"acme/site#81": {
			{SessionID: "s1", CreatedAt: 100},
			{SessionID: "deleted-elsewhere", CreatedAt: 90},
		}},
	}

	views, err := itemSessionsService(manager, links).ItemSessions(t.Context(), 7)
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "s1", views[0].ID)
	assert.Equal(t, []string{"deleted-elsewhere"}, links.unlinked)
}

// A listing that failed proves nothing about what still exists; pruning on it
// would throw associations away because hive.db was momentarily unreadable.
func TestSessionsService_ItemSessionsKeepsLinksWhenHiveCannotBeRead(t *testing.T) {
	manager := &fakeSessionManager{err: errors.New("hive.db locked")}
	links := &fakeItemSessionStore{
		refs:  map[int64]store.ItemRef{7: {ProfileID: "p", ExternalID: "acme/site#81"}},
		links: map[string][]store.ItemSession{"acme/site#81": {{SessionID: "s1", CreatedAt: 100}}},
	}

	_, err := itemSessionsService(manager, links).ItemSessions(t.Context(), 7)
	assert.Equal(t, KindInternal, KindOf(err))
	assert.Empty(t, links.unlinked)
}

// The view is already correct without the prune, so a failed cleanup must not
// hide the sessions that do still exist.
func TestSessionsService_ItemSessionsSurvivesAFailedPrune(t *testing.T) {
	manager := &fakeSessionManager{
		sessions: []dispatch.SessionSummary{{ID: "s1", Name: "kept", Slug: "kept", State: "active"}},
	}
	links := &fakeItemSessionStore{
		refs: map[int64]store.ItemRef{7: {ProfileID: "p", ExternalID: "acme/site#81"}},
		links: map[string][]store.ItemSession{"acme/site#81": {
			{SessionID: "s1", CreatedAt: 100},
			{SessionID: "gone", CreatedAt: 90},
		}},
		unlinkErr: errors.New("disk full"),
	}

	views, err := itemSessionsService(manager, links).ItemSessions(t.Context(), 7)
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "s1", views[0].ID)
}

// Liveness is the last thing added to an otherwise complete answer, so losing
// it must not cost the sessions themselves — the frontend renders a failed read
// as an empty pane.
func TestSessionsService_ItemSessionsKeepsSessionsWhenLivenessCannotBeRead(t *testing.T) {
	manager := &fakeSessionManager{
		sessions:   []dispatch.SessionSummary{{ID: "s1", Name: "kept", Slug: "kept", State: "active"}},
		runningErr: errors.New("tmux is not reachable"),
	}
	links := &fakeItemSessionStore{
		refs:  map[int64]store.ItemRef{7: {ProfileID: "p", ExternalID: "acme/site#81"}},
		links: map[string][]store.ItemSession{"acme/site#81": {{SessionID: "s1", CreatedAt: 100}}},
	}

	views, err := itemSessionsService(manager, links).ItemSessions(t.Context(), 7)
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "s1", views[0].ID)
	assert.False(t, views[0].Running)
}

func TestSessionsService_ItemSessionsRejectsAnUnknownItem(t *testing.T) {
	manager, _ := activeSession()
	links := &fakeItemSessionStore{refs: map[int64]store.ItemRef{}}

	_, err := itemSessionsService(manager, links).ItemSessions(t.Context(), 404)
	assert.Equal(t, KindNotFound, KindOf(err))
}

// A New Session form drafted from an item hands the launcher that item, which
// is what makes the created session findable from it afterwards.
func TestSessionsService_CreateSessionCarriesTheDraftedItem(t *testing.T) {
	launcher := &fakeSessionLauncher{}
	manager, _ := activeSession()
	ref := store.ItemRef{ProfileID: "p", SourceKind: "github", ExternalID: "acme/site#81"}
	svc := &sessionsDeps{
		launcher: launcher, manager: manager, statuses: manager, tmux: &fakeSessionTmux{},
		jobs: &fakeJobRunner{}, links: &fakeItemSessionStore{refs: map[int64]store.ItemRef{7: ref}},
		logger: zerolog.Nop(),
	}

	_, err := svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Repository: "r", Name: "review-81", ItemID: 7})
	require.NoError(t, err)
	require.Len(t, launcher.calls, 1)
	assert.Equal(t, ref, launcher.calls[0].Origin)
}

// An item pruned between opening the form and submitting it must not cost the
// user the session they asked for.
func TestSessionsService_CreateSessionLaunchesUnlinkedWhenTheItemHasGone(t *testing.T) {
	launcher := &fakeSessionLauncher{}
	manager, _ := activeSession()
	svc := &sessionsDeps{
		launcher: launcher, manager: manager, statuses: manager, tmux: &fakeSessionTmux{},
		jobs: &fakeJobRunner{}, links: &fakeItemSessionStore{refs: map[int64]store.ItemRef{}},
		logger: zerolog.Nop(),
	}

	_, err := svc.CreateSession(t.Context(), dispatch.CreateSessionRequest{Repository: "r", Name: "review-81", ItemID: 404})
	require.NoError(t, err)
	require.Len(t, launcher.calls, 1)
	assert.Equal(t, store.ItemRef{}, launcher.calls[0].Origin)
}
