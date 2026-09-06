package stores

import (
	"encoding/json"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// InboxItem is the read-side shape used by inbox callers.
type InboxItem struct {
	ID             int64           `json:"id"`
	ProfileID      string          `json:"profileId"`
	SourceKind     string          `json:"sourceKind"`
	SourceScope    string          `json:"sourceScope"`
	ExternalID     string          `json:"externalId"`
	Title          string          `json:"title"`
	URL            string          `json:"url"`
	Payload        json.RawMessage `json:"payload"`
	Revision       int64           `json:"revision"`
	Unread         bool            `json:"unread"`
	ArchivedAt     *int64          `json:"archivedAt,omitempty"`
	ArchivedActor  string          `json:"archivedActor,omitempty"  jsonschema:"enum=manual,enum=system,description=Who archived the item; present only when archived."`
	ArchivedReason string          `json:"archivedReason,omitempty"`
	Lifecycle      string          `json:"lifecycle"                jsonschema:"enum=active,enum=terminal,enum=unknown,description=Whether the underlying item is still live (active) or resolved (terminal)."`
	SourceState    string          `json:"sourceState,omitempty"`
	FirstSeenAt    int64           `json:"firstSeenAt"`
	LastEventAt    int64           `json:"lastEventAt"`
	IgnoredAt      *int64          `json:"ignoredAt,omitempty"`
}

type InboxEvent struct {
	ID         int64           `json:"id"`
	ItemID     int64           `json:"itemId"`
	Kind       string          `json:"kind"`
	Transition string          `json:"transition"        jsonschema:"enum=none,enum=entered-terminal,enum=left-terminal,description=Lifecycle transition this event marks."`
	Attention  string          `json:"attention"         jsonschema:"enum=activity,enum=trivial,description=Whether the event is worth surfacing (activity) or routine (trivial)."`
	Summary    string          `json:"summary,omitempty"`
	Detail     json.RawMessage `json:"detail,omitempty"`
	CreatedAt  int64           `json:"createdAt"`
}

type FeedCount struct {
	FeedID   string `json:"feedId"`
	Total    int64  `json:"total"`
	Unread   int64  `json:"unread"`
	Archived int64  `json:"archived"`
}

// IngestObservationParams is InboxItemStore.IngestObservation's input: one
// source item as the connector saw it, plus the topic and archive-resurface
// policy the write is scoped to.
type IngestObservationParams struct {
	ProfileID string
	Topic     string
	Policy    models.ResurfacePolicy
	Current   models.Observation
}

// IngestResult is what IngestObservation wrote. A duplicate payload against
// the recorded source head is a deliberate no-op, reported as a zero value
// with Wrote false rather than an error.
type IngestResult struct {
	ItemID         int64
	Revision       int64
	Classification models.Classification
	Wrote          bool
	Offset         int64
}

// InboxItemSynthesize mints a durable row for a feed output whose key never
// went through ingest -- a function node minted it while splitting one
// source message into per-entity items. See InboxItemStore.CreateSynthesized.
type InboxItemSynthesize struct {
	ProfileID   string
	SourceKind  string
	SourceScope string
	ExternalID  string
	Payload     []byte
	// Now stamps first_seen_at and last_event_at. EventLogStore.Commit passes
	// the one clock reading it took for the whole batch, so every item minted
	// in one commit agrees with the node runs and KV writes beside it.
	Now int64
}

// maxEventDetailBytes bounds inbox_event.detail. IngestObservation is its
// only caller.
const maxEventDetailBytes = 4096

func boundEventDetail(detail []byte) []byte {
	if len(detail) <= maxEventDetailBytes {
		return detail
	}
	return append([]byte(nil), detail[:maxEventDetailBytes]...)
}

func mapInboxItemFromDB(row queries.InboxItem) InboxItem {
	item := InboxItem{
		ID: row.ID, ProfileID: row.ProfileID, SourceKind: row.SourceKind,
		SourceScope: row.SourceScope, ExternalID: row.ExternalID, Title: row.Title,
		URL: row.Url, Payload: json.RawMessage(row.Payload), Revision: row.Revision,
		Unread: row.Unread != 0, Lifecycle: row.Lifecycle, FirstSeenAt: row.FirstSeenAt,
		LastEventAt: row.LastEventAt,
	}
	if row.ArchivedAt.Valid {
		archivedAt := row.ArchivedAt.Int64
		item.ArchivedAt = &archivedAt
	}
	if row.ArchivedActor.Valid {
		item.ArchivedActor = row.ArchivedActor.String
	}
	if row.ArchivedReason.Valid {
		item.ArchivedReason = row.ArchivedReason.String
	}
	if row.SourceState.Valid {
		item.SourceState = row.SourceState.String
	}
	if row.IgnoredAt.Valid {
		ignoredAt := row.IgnoredAt.Int64
		item.IgnoredAt = &ignoredAt
	}
	return item
}

func mapInboxEventFromDB(row queries.InboxEvent) InboxEvent {
	event := InboxEvent{ID: row.ID, ItemID: row.ItemID, Kind: row.Kind, Transition: row.Transition, Attention: row.Attention, CreatedAt: row.CreatedAt}
	if row.Summary.Valid {
		event.Summary = row.Summary.String
	}
	if len(row.Detail) > 0 {
		event.Detail = json.RawMessage(row.Detail)
	}
	return event
}
