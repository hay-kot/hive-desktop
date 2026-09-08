package stores

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// InboxItemStore owns inbox_item and inbox_event: the durable substrate
// behind every feed item, and the lifecycle events recorded against it.
// IngestObservation also touches source_head and event_log, and
// ResolveScoped moves item_session links, each inside its own transaction,
// which is why this store holds those siblings (clause 2: an aggregate's own
// store may open a transaction and call sibling stores when the write
// belongs to it).
type InboxItemStore struct {
	q        *queries.DB
	heads    *SourceHeadStore
	sessions *ItemSessionStore
	// log is set by New after both stores exist: EventLogStore.Commit and
	// ActivateReplay resolve and mint through InboxItemStore, so neither
	// store can be fully built before the other.
	log    *EventLogStore
	now    func() time.Time
	mapper MapFunc[queries.InboxItem, InboxItem]
}

func NewInboxItemStore(q *queries.DB, opts Options, heads *SourceHeadStore, sessions *ItemSessionStore) *InboxItemStore {
	return &InboxItemStore{q: q, heads: heads, sessions: sessions, now: opts.Now, mapper: mapInboxItemFromDB}
}

// ListByFeed returns a feed's active items, newest first.
func (s *InboxItemStore) ListByFeed(ctx context.Context, profileID, feedID string, limit int) ([]InboxItem, error) {
	if limit <= 0 {
		return []InboxItem{}, nil
	}
	rows, err := s.q.Ctx(ctx).ListInboxItemsByFeed(ctx, queries.ListInboxItemsByFeedParams{ProfileID: profileID, FeedID: feedID, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("listing inbox items for feed %q: %w", feedID, err)
	}
	return s.mapper.Slice(rows), nil
}

// ListArchivedByFeed returns the feed's archived section, newest archive
// first. It is queried lazily when the archived divider expands.
func (s *InboxItemStore) ListArchivedByFeed(ctx context.Context, profileID, feedID string, limit int) ([]InboxItem, error) {
	if limit <= 0 {
		return []InboxItem{}, nil
	}
	rows, err := s.q.Ctx(ctx).ListArchivedInboxItemsByFeed(ctx, queries.ListArchivedInboxItemsByFeedParams{ProfileID: profileID, FeedID: feedID, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("listing archived inbox items for feed %q: %w", feedID, err)
	}
	return s.mapper.Slice(rows), nil
}

// ListTrash returns unrouted (zero feed claims) and user-ignored items.
// Trash is a utility/debug surface, not a work queue: it carries no unread
// semantics.
func (s *InboxItemStore) ListTrash(ctx context.Context, profileID string, limit int) ([]InboxItem, error) {
	if limit <= 0 {
		return []InboxItem{}, nil
	}
	rows, err := s.q.Ctx(ctx).ListInboxItemsTrash(ctx, queries.ListInboxItemsTrashParams{ProfileID: profileID, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("listing trash inbox items for %q: %w", profileID, err)
	}
	return s.mapper.Slice(rows), nil
}

// ListAll returns every inbox item newest-first, optionally scoped to one
// profile (empty profileID matches all). It is unfiltered by feed membership
// or triage state -- a debug/observation read, not a workspace view.
func (s *InboxItemStore) ListAll(ctx context.Context, profileID string, limit int) ([]InboxItem, error) {
	if limit <= 0 {
		return []InboxItem{}, nil
	}
	rows, err := s.q.Ctx(ctx).ListAllInboxItems(ctx, queries.ListAllInboxItemsParams{ProfileID: profileID, Lim: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("listing all inbox items: %w", err)
	}
	return s.mapper.Slice(rows), nil
}

// ListUnarchived returns exactly the Wails-safe items eligible for synthetic
// replay. Archived memberships are deliberately frozen and never returned.
func (s *InboxItemStore) ListUnarchived(ctx context.Context, profileID string) ([]InboxItem, error) {
	rows, err := s.q.Ctx(ctx).ListUnarchivedInboxItemsByProfile(ctx, profileID)
	if err != nil {
		return nil, fmt.Errorf("listing unarchived inbox items for %q: %w", profileID, err)
	}
	return s.mapper.Slice(rows), nil
}

// ListUnarchivedBySource returns the unarchived items behind one connector
// instance, for reconciling a webhook delivery's authoritative snapshot.
func (s *InboxItemStore) ListUnarchivedBySource(ctx context.Context, profileID, sourceKind, sourceScope string) ([]InboxItem, error) {
	rows, err := s.q.Ctx(ctx).ListUnarchivedInboxItemsBySource(ctx, queries.ListUnarchivedInboxItemsBySourceParams{
		ProfileID: profileID, SourceKind: sourceKind, SourceScope: sourceScope,
	})
	if err != nil {
		return nil, fmt.Errorf("listing unarchived inbox items for source %s/%s: %w", sourceKind, sourceScope, err)
	}
	return s.mapper.Slice(rows), nil
}

// FindByExternalID returns every item sharing an external id, optionally
// scoped to one profile (empty profileID matches all). The same id can exist
// across profiles and source scopes, so this returns a slice.
func (s *InboxItemStore) FindByExternalID(ctx context.Context, profileID, externalID string) ([]InboxItem, error) {
	rows, err := s.q.Ctx(ctx).FindInboxItemsByExternalID(ctx, queries.FindInboxItemsByExternalIDParams{
		ExternalID: externalID, ProfileID: profileID,
	})
	if err != nil {
		return nil, fmt.Errorf("finding inbox items for external id %q: %w", externalID, err)
	}
	return s.mapper.Slice(rows), nil
}

// GetByID reads one inbox item by its row id.
func (s *InboxItemStore) GetByID(ctx context.Context, id int64) (InboxItem, error) {
	row, err := s.q.Ctx(ctx).GetInboxItemByID(ctx, id)
	return s.mapper.Err(row, errTransformQueryOne("inbox_item", fmt.Sprint(id), err))
}

// RefByID resolves an inbox row to the ref an association is keyed on.
func (s *InboxItemStore) RefByID(ctx context.Context, itemID int64) (models.ItemRef, error) {
	row, err := s.q.Ctx(ctx).GetInboxItemByID(ctx, itemID)
	if err != nil {
		return models.ItemRef{}, errTransformQueryOne("inbox_item", fmt.Sprint(itemID), err)
	}
	return models.ItemRef{
		ProfileID:   row.ProfileID,
		SourceKind:  row.SourceKind,
		SourceScope: row.SourceScope,
		ExternalID:  row.ExternalID,
	}, nil
}

// ResolveScoped resolves the durable inbox row behind a source identity,
// healing pre-#63 rows on the way.
//
// #63 gave GitHub observations a SourceScope (the account); rows written
// before it carry an empty source_scope and are invisible to the scoped
// lookup every post-#63 read keys on. On a scoped miss this retries under
// the empty scope, and if that hits it rewrites the row to the requested
// scope so the identity -- and the triage decisions the row carries -- are
// consistent for every later read, and so a subsequent ingest updates the
// row in place rather than inserting a scoped duplicate beside it. A genuine
// miss returns sql.ErrNoRows unchanged. See issue #95.
func (s *InboxItemStore) ResolveScoped(ctx context.Context, profileID, sourceKind, sourceScope, externalID string) (InboxItem, error) {
	item, err := s.q.Ctx(ctx).GetInboxItemByExternalID(ctx, queries.GetInboxItemByExternalIDParams{
		ProfileID: profileID, SourceKind: sourceKind, SourceScope: sourceScope, ExternalID: externalID,
	})
	if err == nil {
		return s.mapper(item), nil
	}
	if !errors.Is(err, sql.ErrNoRows) || sourceScope == "" {
		return InboxItem{}, err
	}

	var legacy queries.InboxItem
	txErr := s.q.WithinTx(ctx, func(ctx context.Context, q *queries.DB) error {
		var legacyErr error
		legacy, legacyErr = q.GetInboxItemByExternalID(ctx, queries.GetInboxItemByExternalIDParams{
			ProfileID: profileID, SourceKind: sourceKind, SourceScope: "", ExternalID: externalID,
		})
		if legacyErr != nil {
			if errors.Is(legacyErr, sql.ErrNoRows) {
				return err
			}
			return legacyErr
		}
		if rescopeErr := q.RescopeInboxItem(ctx, queries.RescopeInboxItemParams{SourceScope: sourceScope, ID: legacy.ID}); rescopeErr != nil {
			return rescopeErr
		}
		// item_session is keyed on these same coordinates, so the links have
		// to move with the row or they address a scope nothing reads under
		// again.
		return s.sessions.Rescope(ctx, profileID, sourceKind, externalID, sourceScope)
	})
	if txErr != nil {
		return InboxItem{}, txErr
	}
	legacy.SourceScope = sourceScope
	return s.mapper(legacy), nil
}

// IDByExternalID resolves the durable inbox row behind a source identity. It
// is the read-only half of the identity a commit resolves for feed
// membership, exposed for callers that hold only the source-side identity --
// a delivered notification linking back to the item that triggered it. A
// missing row is (0, nil), not an error: an identity can legitimately have
// no inbox row (a message a function node synthesized), and that only means
// "nothing to link to".
func (s *InboxItemStore) IDByExternalID(ctx context.Context, profileID, sourceKind, sourceScope, externalID string) (int64, error) {
	if externalID == "" {
		return 0, nil
	}
	item, err := s.q.Ctx(ctx).GetInboxItemByExternalID(ctx, queries.GetInboxItemByExternalIDParams{
		ProfileID: profileID, SourceKind: sourceKind, SourceScope: sourceScope, ExternalID: externalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("resolving inbox item %s/%s/%s: %w", sourceKind, sourceScope, externalID, err)
	}
	return item.ID, nil
}

// FeedIDForItem returns the feed that claims an item, or "" when nothing
// does (an unrouted item, which the UI shows in Trash). An item claimed by
// several feeds resolves to the lowest feed id so the answer is stable
// across calls -- any of them reveals the item.
func (s *InboxItemStore) FeedIDForItem(ctx context.Context, profileID string, itemID int64) (string, error) {
	feedID, err := s.q.Ctx(ctx).GetFeedIDForItem(ctx, queries.GetFeedIDForItemParams{ProfileID: profileID, ItemID: itemID})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolving feed for inbox item %d: %w", itemID, err)
	}
	return feedID, nil
}

// FeedIDsForItems resolves the claiming feed of each item in one query,
// using the same lowest-feed-id rule as FeedIDForItem. Keyed by item id (a
// global primary key, so the profile is implied); an item with no claim is
// absent from the map, meaning its feed is "". Built for callers that list
// items flat and need each item's feed without an N+1 of FeedIDForItem.
func (s *InboxItemStore) FeedIDsForItems(ctx context.Context, itemIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := s.q.Ctx(ctx).ListFeedIDsForItems(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("resolving feeds for %d inbox items: %w", len(itemIDs), err)
	}
	for _, row := range rows {
		out[row.ItemID] = row.FeedID
	}
	return out, nil
}

// Events lists one item's lifecycle events, newest first.
func (s *InboxItemStore) Events(ctx context.Context, itemID int64, limit int) ([]InboxEvent, error) {
	if limit <= 0 {
		return []InboxEvent{}, nil
	}
	rows, err := s.q.Ctx(ctx).ListInboxEventsByItem(ctx, queries.ListInboxEventsByItemParams{ItemID: itemID, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("listing inbox events for %d: %w", itemID, err)
	}
	events := make([]InboxEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, mapInboxEventFromDB(row))
	}
	return events, nil
}

// SetUnread sets one item's unread flag under a revision guard. A stale
// revision -- the row has moved since the caller read it -- returns
// ErrStale, not a NotFoundError: it must bypass errTransformQueryOne, or a
// caller checking IsNotFound(err) would see this exact conflict as a missing
// row instead of "re-read and retry".
func (s *InboxItemStore) SetUnread(ctx context.Context, itemID, revision int64, unread bool) (InboxItem, error) {
	row, err := s.q.Ctx(ctx).SetInboxItemUnread(ctx, queries.SetInboxItemUnreadParams{Unread: boolToInt64(unread), ID: itemID, Revision: revision})
	if errors.Is(err, sql.ErrNoRows) {
		return InboxItem{}, ErrStale
	}
	if err != nil {
		return InboxItem{}, fmt.Errorf("setting inbox item %d unread: %w", itemID, err)
	}
	return s.mapper(row), nil
}

// MarkRead clears unread across a whole scope in one statement: feedID names
// a single feed, an empty feedID means every feed in the workspace. It
// returns how many rows it changed so the caller can report the size of
// what it just did.
//
// Callers get no rows back. A bulk clear touches more rows than a UI holds
// and the revisions all move, so the frontend re-reads the affected list and
// the sidebar counts rather than patching what it has.
func (s *InboxItemStore) MarkRead(ctx context.Context, profileID, feedID string) (int64, error) {
	if profileID == "" {
		return 0, fmt.Errorf("marking inbox items read: profile id is required")
	}
	q := s.q.Ctx(ctx)
	if feedID == "" {
		marked, err := q.MarkProfileInboxItemsRead(ctx, profileID)
		if err != nil {
			return 0, fmt.Errorf("marking every feed read for %q: %w", profileID, err)
		}
		return marked, nil
	}
	marked, err := q.MarkFeedInboxItemsRead(ctx, queries.MarkFeedInboxItemsReadParams{ProfileID: profileID, FeedID: feedID})
	if err != nil {
		return 0, fmt.Errorf("marking feed %q read: %w", feedID, err)
	}
	return marked, nil
}

// ToggleArchived flips one item's archived state under a revision guard,
// stamping the store's own clock when archiving. See SetUnread's comment on
// why a stale revision returns ErrStale rather than going through
// errTransformQueryOne.
func (s *InboxItemStore) ToggleArchived(ctx context.Context, itemID, revision int64) (InboxItem, error) {
	row, err := s.q.Ctx(ctx).ToggleInboxItemArchived(ctx, queries.ToggleInboxItemArchivedParams{
		ArchivedAt: sql.NullInt64{Int64: s.now().UnixMilli(), Valid: true}, ID: itemID, Revision: revision,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return InboxItem{}, ErrStale
	}
	if err != nil {
		return InboxItem{}, fmt.Errorf("toggling archive for inbox item %d: %w", itemID, err)
	}
	return s.mapper(row), nil
}

// ToggleIgnored flips one item's ignored state under a revision guard,
// stamping the store's own clock when ignoring. See SetUnread's comment on
// why a stale revision returns ErrStale rather than going through
// errTransformQueryOne.
func (s *InboxItemStore) ToggleIgnored(ctx context.Context, itemID, revision int64) (InboxItem, error) {
	row, err := s.q.Ctx(ctx).ToggleInboxItemIgnored(ctx, queries.ToggleInboxItemIgnoredParams{
		IgnoredAt: sql.NullInt64{Int64: s.now().UnixMilli(), Valid: true}, ID: itemID, Revision: revision,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return InboxItem{}, ErrStale
	}
	if err != nil {
		return InboxItem{}, fmt.Errorf("toggling ignored state for inbox item %d: %w", itemID, err)
	}
	return s.mapper(row), nil
}

// FeedCounts returns total/unread/archived counts per feed for a profile.
func (s *InboxItemStore) FeedCounts(ctx context.Context, profileID string) ([]FeedCount, error) {
	rows, err := s.q.Ctx(ctx).CountInboxItemsByFeed(ctx, profileID)
	if err != nil {
		return nil, fmt.Errorf("counting inbox items by feed for %q: %w", profileID, err)
	}
	counts := make([]FeedCount, 0, len(rows))
	for _, row := range rows {
		counts = append(counts, FeedCount(row))
	}
	return counts, nil
}

// DeleteByProfile removes every inbox_item row for profileID. Used by
// FlowsService.purgeProfile when a workspace is deleted.
func (s *InboxItemStore) DeleteByProfile(ctx context.Context, profileID string) error {
	return wrap("deleting inbox items by profile", s.q.Ctx(ctx).DeleteInboxItemsByProfile(ctx, profileID))
}

// GetUnarchivedByID reads one unarchived inbox row by id, scoped to
// profileID. EventLogStore.ActivateReplay uses this to refuse a claim
// against an item that is archived, missing, or belongs to another profile.
func (s *InboxItemStore) GetUnarchivedByID(ctx context.Context, itemID int64, profileID string) (InboxItem, error) {
	row, err := s.q.Ctx(ctx).GetUnarchivedInboxItemByID(ctx, queries.GetUnarchivedInboxItemByIDParams{ID: itemID, ProfileID: profileID})
	if err != nil {
		return InboxItem{}, errTransformQueryOne("inbox_item", fmt.Sprint(itemID), err)
	}
	return s.mapper(row), nil
}

// CreateSynthesized inserts a durable row for a feed output whose key never
// went through ingest -- a function node minted it while splitting one
// source message into per-entity items. EventLogStore.Commit calls this when
// ResolveScoped finds no row for a feed output's key. Presentation comes
// from the payload (title/url), the same fields the producer reads at the
// ingest boundary; the lifecycle is active because the item is present in
// the snapshot that carried it, and its absence from a later snapshot drops
// the membership claim rather than archiving the row. A subsequent ingest
// under the same identity upserts this row in place, so a genuine source
// item briefly missing at commit self-heals rather than forking a
// duplicate.
func (s *InboxItemStore) CreateSynthesized(ctx context.Context, in InboxItemSynthesize) (InboxItem, error) {
	title, url := feedItemPresentation(in.ExternalID, in.Payload)
	row, err := s.q.Ctx(ctx).InsertInboxItem(ctx, queries.InsertInboxItemParams{
		ProfileID: in.ProfileID, SourceKind: in.SourceKind, SourceScope: in.SourceScope, ExternalID: in.ExternalID,
		Title: title, Url: url, Payload: in.Payload, Unread: 1, Lifecycle: models.LifecycleActive.String(),
		FirstSeenAt: in.Now, LastEventAt: in.Now,
	})
	if err != nil {
		return InboxItem{}, wrap("minting synthesized inbox item", err)
	}
	return s.mapper(row), nil
}

// feedItemPresentation reads the title and url a synthesized feed item
// renders with from its payload, mirroring the ingest boundary's convention.
// A payload with no title falls back to the key, so an item is never blank.
func feedItemPresentation(key string, payload []byte) (title, url string) {
	var wire struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	_ = json.Unmarshal(payload, &wire)
	if title = wire.Title; title == "" {
		title = key
	}
	return title, wire.URL
}

type ItemTriageState struct {
	Unread         bool
	ArchivedAt     *int64
	ArchivedActor  string
	ArchivedReason string
}

// applyTransition is intentionally SQL-free so archive semantics remain easy
// to test. Terminal transitions are system-owned; system archived items
// always return on a reopen, while manual archives obey the profile policy.
func applyTransition(prev ItemTriageState, c models.Classification, policy models.ResurfacePolicy) ItemTriageState {
	next := prev
	if c.Transition == models.TransitionEnteredTerminal {
		// A user archive wins a concurrent terminal observation. The item is
		// already hidden; replacing its actor with system would erase the
		// manual decision the in-transaction read deliberately observed.
		if prev.ArchivedActor == models.ArchivedActorManual.String() && prev.ArchivedAt != nil {
			return next
		}
		now := time.Now().UnixMilli()
		next.ArchivedAt = &now
		next.ArchivedActor = models.ArchivedActorSystem.String()
		next.Unread = false
		return next
	}
	if prev.ArchivedAt == nil {
		if c.Attention == models.AttentionActivity || c.Transition == models.TransitionLeftTerminal {
			next.Unread = true
		}
		return next
	}
	resurface := c.Transition == models.TransitionLeftTerminal ||
		(prev.ArchivedActor == models.ArchivedActorManual.String() && policy == models.ResurfacePolicyAll && c.Attention == models.AttentionActivity)
	if prev.ArchivedActor == models.ArchivedActorSystem.String() && c.Transition == models.TransitionLeftTerminal {
		resurface = true
	}
	if prev.ArchivedActor == models.ArchivedActorManual.String() && policy == models.ResurfacePolicyNever {
		resurface = false
	}
	if resurface {
		next.ArchivedAt = nil
		next.ArchivedActor = ""
		next.Unread = true
	}
	return next
}

// archivedReason retains the reason for an item that remains archived. A
// manual archive predating reason tracking is labeled manual; a newly
// system-archived terminal item takes the classifier's source-specific
// reason.
func archivedReason(prev, next ItemTriageState, c models.Classification) string {
	if next.ArchivedAt == nil {
		return ""
	}
	if prev.ArchivedAt != nil {
		if prev.ArchivedReason != "" {
			return prev.ArchivedReason
		}
		if next.ArchivedActor == models.ArchivedActorManual.String() {
			return models.ArchivedActorManual.String()
		}
		return ""
	}
	if next.ArchivedActor == models.ArchivedActorManual.String() {
		return models.ArchivedActorManual.String()
	}
	return c.ArchivedReason
}

// IngestObservation is the persistence boundary for a source item: the
// source head comparison, classification, inbox mutation, event log append
// and source head update share one immediate SQLite transaction. The
// aggregate it belongs to is the inbox item -- everything else in it exists
// to decide what that row becomes -- so it writes source_head through
// SourceHeadStore and the event log through EventLogStore rather than
// reaching their generated queries directly.
func (s *InboxItemStore) IngestObservation(ctx context.Context, classifier models.Classifier, p IngestObservationParams) (result IngestResult, err error) {
	if classifier == nil {
		return result, fmt.Errorf("ingesting observation: nil classifier")
	}
	if p.Current.ExternalID == "" {
		return result, fmt.Errorf("ingesting observation: external id is required")
	}
	if p.Policy == "" {
		p.Policy = models.ResurfacePolicyStateChanges
	}
	err = s.q.WithinTx(ctx, func(ctx context.Context, _ *queries.DB) error {
		head, headErr := s.heads.Payload(ctx, p.Topic, p.Current.ExternalID)
		if headErr == nil && bytes.Equal(head, p.Current.Payload) {
			return nil
		}
		if headErr != nil && !errors.Is(headErr, sql.ErrNoRows) {
			return fmt.Errorf("reading source head: %w", headErr)
		}

		var previous *models.Observation
		prevRow, getErr := s.ResolveScoped(ctx, p.ProfileID, p.Current.SourceKind, p.Current.SourceScope, p.Current.ExternalID)
		if getErr == nil {
			previous = &models.Observation{ExternalID: prevRow.ExternalID, Title: prevRow.Title, URL: prevRow.URL, SourceKind: prevRow.SourceKind, SourceScope: prevRow.SourceScope, ObservedAt: prevRow.LastEventAt, Payload: prevRow.Payload}
		} else if !errors.Is(getErr, sql.ErrNoRows) {
			return fmt.Errorf("reading inbox item: %w", getErr)
		}

		classification := classifier.Classify(previous, p.Current)
		if classification.Transition == "" {
			classification.Transition = models.TransitionNone
		}
		if classification.Attention == "" {
			classification.Attention = models.AttentionTrivial
		}
		if classification.Lifecycle == "" {
			classification.Lifecycle = models.LifecycleUnknown
		}
		classification.Detail = boundEventDetail(classification.Detail)
		prevTriage := ItemTriageState{}
		if getErr == nil {
			prevTriage.Unread = prevRow.Unread
			prevTriage.ArchivedAt = prevRow.ArchivedAt
			prevTriage.ArchivedActor = prevRow.ArchivedActor
			prevTriage.ArchivedReason = prevRow.ArchivedReason
		}
		triage := applyTransition(prevTriage, classification, p.Policy)
		archiveReason := archivedReason(prevTriage, triage, classification)
		now := s.now().UnixMilli()
		var archivedAt sql.NullInt64
		if triage.ArchivedAt != nil {
			archivedAt = sql.NullInt64{Int64: *triage.ArchivedAt, Valid: true}
		}
		item, upsertErr := s.q.Ctx(ctx).UpsertInboxItem(ctx, queries.UpsertInboxItemParams{
			ProfileID: p.ProfileID, SourceKind: p.Current.SourceKind, SourceScope: p.Current.SourceScope, ExternalID: p.Current.ExternalID,
			Title: p.Current.Title, Url: p.Current.URL, Payload: p.Current.Payload, Unread: boolToInt64(triage.Unread),
			ArchivedAt: archivedAt, ArchivedActor: null(triage.ArchivedActor), ArchivedReason: null(archiveReason),
			Lifecycle: classification.Lifecycle.String(), SourceState: null(classification.SourceState), FirstSeenAt: now, LastEventAt: p.Current.ObservedAt,
		})
		if upsertErr != nil {
			return fmt.Errorf("upserting inbox item: %w", upsertErr)
		}

		if classification.Transition != models.TransitionNone || classification.Attention != models.AttentionTrivial {
			var occurrence sql.NullString
			if classification.OccurrenceKey != "" {
				occurrence = sql.NullString{String: classification.OccurrenceKey, Valid: true}
			}
			_, eventErr := s.q.Ctx(ctx).InsertInboxEvent(ctx, queries.InsertInboxEventParams{ItemID: item.ID, Kind: classification.Kind, Transition: classification.Transition.String(), Attention: classification.Attention.String(), OccurrenceKey: occurrence, Summary: null(classification.Summary), Detail: classification.Detail, CreatedAt: now})
			if eventErr != nil && !errors.Is(eventErr, sql.ErrNoRows) {
				return fmt.Errorf("inserting inbox event: %w", eventErr)
			}
		}

		occurrence := classification.OccurrenceKey
		offset, appendErr := s.log.AppendObservation(ctx, p.Topic, p.Current.ExternalID, p.Current.Payload, p.Current.SourceKind, p.Current.SourceScope, occurrence, now)
		if appendErr != nil {
			return fmt.Errorf("appending event log: %w", appendErr)
		}
		if occurrence == "" {
			occurrence = fmt.Sprintf("%d", offset)
			if err := s.log.BackfillOccurrenceKey(ctx, offset, occurrence); err != nil {
				return fmt.Errorf("backfilling occurrence key: %w", err)
			}
		}
		if err := s.heads.Upsert(ctx, p.Topic, p.Current.ExternalID, p.Current.Payload); err != nil {
			return fmt.Errorf("updating source head: %w", err)
		}
		result = IngestResult{ItemID: item.ID, Revision: item.Revision, Classification: classification, Wrote: true, Offset: offset}
		return nil
	})
	if err != nil {
		return IngestResult{}, fmt.Errorf("ingesting observation: %w", err)
	}
	return result, nil
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
