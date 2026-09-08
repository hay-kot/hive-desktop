package stores

import (
	"context"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// ItemSessionStore owns item_session: the durable association between an
// inbox item and the hive sessions launched for it.
type ItemSessionStore struct {
	q   *queries.DB
	now func() time.Time
}

func NewItemSessionStore(q *queries.DB, opts Options) *ItemSessionStore {
	return &ItemSessionStore{q: q, now: opts.Now}
}

// Link records that a hive session was created on ref's behalf. Nothing
// about the session is copied: hive owns its name, state and checkout, and a
// mirror here could only go stale.
func (s *ItemSessionStore) Link(ctx context.Context, sessionID string, ref models.ItemRef) error {
	if sessionID == "" || !ref.Known() {
		return nil
	}
	err := s.q.Ctx(ctx).LinkItemSession(ctx, queries.LinkItemSessionParams{
		SessionID:   sessionID,
		ProfileID:   ref.ProfileID,
		SourceKind:  ref.SourceKind,
		SourceScope: ref.SourceScope,
		ExternalID:  ref.ExternalID,
		CreatedAt:   s.now().UnixMilli(),
	})
	return wrap("linking session to inbox item", err)
}

// List returns the links recorded for ref, newest first. A link is a claim,
// not a guarantee: hive may no longer have the session, which is what
// Unlink resolves.
func (s *ItemSessionStore) List(ctx context.Context, ref models.ItemRef) ([]ItemSession, error) {
	if !ref.Known() {
		return []ItemSession{}, nil
	}
	rows, err := s.q.Ctx(ctx).ListItemSessions(ctx, queries.ListItemSessionsParams(ref))
	if err != nil {
		return nil, wrap("listing sessions for an inbox item", err)
	}
	return MapFunc[queries.ItemSession, ItemSession](mapItemSessionFromDB).Slice(rows), nil
}

// Unlink drops links to sessions hive no longer has. Callers must only pass
// ids a *successful* session listing failed to account for -- a listing that
// errored proves nothing about what still exists.
func (s *ItemSessionStore) Unlink(ctx context.Context, sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	q := s.q.Ctx(ctx)
	for _, id := range sessionIDs {
		if err := q.DeleteItemSession(ctx, id); err != nil {
			return wrap("unlinking a session from its inbox item", err)
		}
	}
	return nil
}

// DeleteByProfile removes every item_session row for profileID. Used by
// FlowsService.purgeProfile when a workspace is deleted; the sessions
// themselves are hive's and survive, only the links go.
func (s *ItemSessionStore) DeleteByProfile(ctx context.Context, profileID string) error {
	return wrap("deleting item sessions by profile", s.q.Ctx(ctx).DeleteItemSessionsByProfile(ctx, profileID))
}

// Rescope moves the links recorded under the empty scope for (profileID,
// sourceKind, externalID) to scope, so they keep addressing the item after
// InboxItemStore.ResolveScoped rewrites the row.
func (s *ItemSessionStore) Rescope(ctx context.Context, profileID, sourceKind, externalID, scope string) error {
	return wrap("rescoping item sessions", s.q.Ctx(ctx).RescopeItemSessions(ctx, queries.RescopeItemSessionsParams{
		SourceScope: scope, ProfileID: profileID, SourceKind: sourceKind, ExternalID: externalID,
	}))
}
