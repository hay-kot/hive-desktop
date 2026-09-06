package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/hay-kot/hive-desktop/internal/app/observe"
)

// Sink kinds: where a committed Output is written.
const (
	SinkKindFeed   = "feed"
	SinkKindAction = "action"
	SinkKindNotify = "notify"
)

// Sink identifies where an Output is committed. Feed outputs claim immutable
// inbox membership; action and notify outputs enqueue an output_command.
type Sink struct {
	Kind     string `json:"kind"`
	TargetID string `json:"targetId"`
}

// NotifyActionPrefix namespaces the synthetic action id a notify terminal
// enqueues its output_command under, so one queue serves both authored
// actions.yml actions and inline notify nodes. Authored action ids are slugs
// (`^[a-z0-9][a-z0-9-]*$`), which can contain neither ":" nor "/", so a
// synthetic id can never collide with one.
const NotifyActionPrefix = "notify:"

// NotifyActionID returns the synthetic action id for a notify node, whose
// target is the flow-qualified node id "<flowId>/<nodeId>".
func NotifyActionID(target string) string { return NotifyActionPrefix + target }

// NotifyActionTarget inverts NotifyActionID: it returns the flow-qualified
// node id an action id names, and whether the id is a notify action at all.
func NotifyActionTarget(actionID string) (string, bool) {
	target, ok := strings.CutPrefix(actionID, NotifyActionPrefix)
	return target, ok && target != ""
}

// NotifyCommand is the durable payload of a notify terminal's
// output_command. It carries the triggering item's identity alongside its
// payload: the identity resolves the inbox row a delivered notification
// links back to (so clicking the banner can select that item), while Item is
// what the node's title/body templates render over — the same shape an
// action node's executor sees, so `{{ .Payload.title }}` means the same
// thing in both.
type NotifyCommand struct {
	ProfileID   string          `json:"profileId"`
	ExternalID  string          `json:"externalId,omitempty"`
	SourceKind  string          `json:"sourceKind,omitempty"`
	SourceScope string          `json:"sourceScope,omitempty"`
	Item        json.RawMessage `json:"item,omitempty"`
}

// Output is one committed side effect of a flow run.
type Output struct {
	Sink          Sink            `json:"sink"`
	Key           string          `json:"key,omitempty"`           // the item's external ID; not set by a keyless snapshot boundary
	OccurrenceKey string          `json:"occurrenceKey,omitempty"` // action dedup key only
	Payload       json.RawMessage `json:"payload,omitempty"`       // action payload only
	SourceKind    string          `json:"sourceKind,omitempty"`
	SourceScope   string          `json:"sourceScope,omitempty"`
	SourceTopic   string          `json:"sourceTopic"`
	SnapshotID    string          `json:"snapshotId,omitempty"`
}

// FeedSnapshot declares one source's complete current output scope for a feed.
// CommitBatch accepts these declarations but does not persist reconciliation
// state until membership claims are introduced.
type FeedSnapshot struct {
	FeedID      string `json:"feedId"`
	SourceTopic string `json:"sourceTopic"`
	SnapshotID  string `json:"snapshotId"`
}

// Discard records a message a node dropped instead of forwarding, for
// metrics/observability. CommitBatch does not persist Discards as rows —
// they exist for callers that want to log or count them; the per-node
// aggregate is expected to already be reflected in the corresponding
// NodeRun.DropCount.
type Discard struct {
	MsgID  string `json:"msgId"`
	NodeID string `json:"nodeId"`
}

// NodeRunView is one node's per-tick execution summary, recorded for the
// flows debug/status UI. It is named "View" (rather than NodeRun) only to
// avoid colliding with the sqlc-generated raw row model of the same name in
// models.go — package pipeline's NodeRun alias re-exports this type under
// the name callers actually use (see pipeline/commit.go).
type NodeRunView struct {
	FlowID    string `json:"flowId"`
	NodeID    string `json:"nodeId"`
	OK        bool   `json:"ok"`
	InCount   int    `json:"inCount"`
	OutCount  int    `json:"outCount"`
	DropCount int    `json:"dropCount"`
	Err       string `json:"err"`
	DurMs     int64  `json:"durMs"`
}

// KVMutation is one node's durable KV write, flushed inside CommitBatch so
// it is durable iff the tick that produced it commits. The row's flow_id is
// CommitBatch.Consumer.
type KVMutation struct {
	NodeID    string `json:"nodeId"`
	Key       string `json:"key"`
	Delete    bool   `json:"delete,omitempty"`
	Value     string `json:"value,omitempty"`
	ExpiresAt int64  `json:"expiresAt,omitempty"` // unix ms, 0 = no expiry
}

// CommitBatch is the graph runtime's (internal/app/runtime) atomic write: it
// advances a consumer's committed offset and persists the outputs/node-run
// metrics produced while processing up to that offset, all in one transaction
// (see DB.CommitBatch).
type CommitBatch struct {
	Consumer      string         `json:"consumer"` // event_log consumer key (flow id / consumer id)
	UpToOffset    int64          `json:"upToOffset"`
	Outputs       []Output       `json:"outputs"`
	FeedSnapshots []FeedSnapshot `json:"feedSnapshots"`
	Discards      []Discard      `json:"discards"`
	NodeRuns      []NodeRunView  `json:"nodeRuns"`
	KVMutations   []KVMutation   `json:"kvMutations,omitempty"` // omitempty: invisible to fixtures with none
}

// CommitBatch applies b atomically: feed outputs resolve their inbox item and
// claim membership, snapshots reconcile only their (feed, source) scope,
// action outputs are enqueued by occurrence key, node runs are recorded, and
// the consumer offset advances to b.UpToOffset.
//
// Idempotency by offset: if b.UpToOffset is at or below the consumer's
// currently committed offset, this batch was already applied in a previous
// commit and the call is a no-op without touching output_command or node_run.
// Only output_command needs its own dedup key, since two different batches
// could legitimately enqueue the same action.
func (db *DB) CommitBatch(ctx context.Context, b CommitBatch) error {
	ctx, span := observe.StartConditionalSpan(ctx, tracer, "db.CommitBatch")
	defer span.End()
	span.SetAttributes(attribute.Int("db.batch.outputs", len(b.Outputs)))

	db.debugPauseCommit(ctx)
	if b.UpToOffset < 0 {
		return fmt.Errorf("commit offset must not be negative: %d", b.UpToOffset)
	}

	return db.WithinTx(ctx, func(ctx context.Context, tx *DB) error {
		current, err := tx.queries.GetConsumerOffset(ctx, b.Consumer)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("reading committed offset for consumer %q: %w", b.Consumer, err)
		}

		if b.UpToOffset <= current.Offset {
			// Already applied by a previous commit of this batch (or a
			// stale/out-of-order commit) — no-op.
			return nil
		}

		now := time.Now().UnixMilli()

		for _, out := range b.Outputs {
			switch out.Sink.Kind {
			case SinkKindFeed:
				item, err := resolveInboxItemScoped(ctx, tx.queries, b.Consumer, out.SourceKind, out.SourceScope, out.Key)
				if errors.Is(err, sql.ErrNoRows) {
					// A feed output whose key has no inbox row is one a function
					// node synthesized: it split a source message into per-entity
					// items under keys the producer never ingested, so no row was
					// minted at the boundary. Mint one here from the payload it
					// carried. A key the producer did ingest resolves above, so
					// its classifier-owned row is left untouched.
					//
					// A key-less output (the omitempty snapshot-boundary row of
					// issue #95) still has no identity to mint under and is
					// skipped — logged, and the offset advances rather than
					// wedging on it. Minting never errors, so the synthesized
					// path keeps the same anti-wedge property the skip gave.
					if out.Key == "" {
						db.logger.Warn().
							Str("consumer", b.Consumer).
							Str("sourceKind", out.SourceKind).
							Str("sourceScope", out.SourceScope).
							Msg("commit: feed output has no key; skipping so the offset can advance")
						continue
					}
					item, err = mintFeedInboxItem(ctx, tx.queries, b.Consumer, out, now)
					if err != nil {
						return fmt.Errorf("minting inbox item %s/%s/%s: %w", out.SourceKind, out.SourceScope, out.Key, err)
					}
				} else if err != nil {
					return fmt.Errorf("resolving inbox item %s/%s/%s: %w", out.SourceKind, out.SourceScope, out.Key, err)
				}
				if err := tx.queries.UpsertFeedMembershipClaim(ctx, UpsertFeedMembershipClaimParams{
					ProfileID: b.Consumer, FeedID: out.Sink.TargetID, ItemID: item.ID, SourceID: out.SourceTopic,
				}); err != nil {
					return fmt.Errorf("claiming feed membership %s/%s: %w", out.Sink.TargetID, out.Key, err)
				}
			case SinkKindAction:
				// The dedup key is the occurrence key, so the row cannot be
				// traced back to its item by key alone.
				if err := tx.queries.EnqueueOutputCommand(ctx, EnqueueOutputCommandParams{
					ActionID: out.Sink.TargetID, Key: out.OccurrenceKey, Payload: []byte(out.Payload), CreatedAt: now,
					ProfileID: b.Consumer, SourceKind: out.SourceKind, SourceScope: out.SourceScope, ExternalID: out.Key,
				}); err != nil {
					return fmt.Errorf("enqueuing output_command %s/%s: %w", out.Sink.TargetID, out.OccurrenceKey, err)
				}
			case SinkKindNotify:
				payload, err := json.Marshal(NotifyCommand{
					ProfileID:   b.Consumer,
					ExternalID:  out.Key,
					SourceKind:  out.SourceKind,
					SourceScope: out.SourceScope,
					Item:        out.Payload,
				})
				if err != nil {
					return fmt.Errorf("encoding notify command %s/%s: %w", out.Sink.TargetID, out.Key, err)
				}
				key := notifyDedupKey(out)
				if err := tx.queries.EnqueueOutputCommand(ctx, EnqueueOutputCommandParams{
					ActionID: NotifyActionID(out.Sink.TargetID), Key: key, Payload: payload, CreatedAt: now,
				}); err != nil {
					return fmt.Errorf("enqueuing notify command %s/%s: %w", out.Sink.TargetID, key, err)
				}
			default:
				return fmt.Errorf("commit batch: unknown sink kind %q", out.Sink.Kind)
			}
		}

		for _, snapshot := range b.FeedSnapshots {
			itemIDs := make([]int64, 0)
			for _, out := range b.Outputs {
				if out.Sink.Kind != SinkKindFeed || out.Sink.TargetID != snapshot.FeedID || out.SourceTopic != snapshot.SourceTopic || out.SnapshotID != snapshot.SnapshotID {
					continue
				}
				item, err := resolveInboxItemScoped(ctx, tx.queries, b.Consumer, out.SourceKind, out.SourceScope, out.Key)
				if errors.Is(err, sql.ErrNoRows) {
					// Consistent with the outputs pass above (already logged
					// there): an item with no row claims no membership, so it
					// contributes nothing to reconcile against.
					continue
				}
				if err != nil {
					return fmt.Errorf("resolving snapshot inbox item %s: %w", out.Key, err)
				}
				itemIDs = append(itemIDs, item.ID)
			}
			if len(itemIDs) == 0 {
				if err := tx.queries.DeleteFeedMembershipClaimsForSourceAll(ctx, DeleteFeedMembershipClaimsForSourceAllParams{FeedID: snapshot.FeedID, SourceID: snapshot.SourceTopic}); err != nil {
					return fmt.Errorf("clearing empty feed snapshot: %w", err)
				}
			} else if err := tx.queries.DeleteFeedMembershipClaimsNotInSnapshot(ctx, DeleteFeedMembershipClaimsNotInSnapshotParams{FeedID: snapshot.FeedID, SourceID: snapshot.SourceTopic, ItemIds: itemIDs}); err != nil {
				return fmt.Errorf("reconciling feed snapshot: %w", err)
			}
		}

		for _, m := range b.KVMutations {
			if m.Delete {
				if err := tx.queries.DeleteNodeKV(ctx, DeleteNodeKVParams{FlowID: b.Consumer, NodeID: m.NodeID, Scope: KVScopeNode, Key: m.Key}); err != nil {
					return fmt.Errorf("deleting node kv %s/%s: %w", m.NodeID, m.Key, err)
				}
				continue
			}
			var expiresAt sql.NullInt64
			if m.ExpiresAt > 0 {
				expiresAt = sql.NullInt64{Int64: m.ExpiresAt, Valid: true}
			}
			if err := tx.queries.UpsertNodeKV(ctx, UpsertNodeKVParams{
				FlowID: b.Consumer, NodeID: m.NodeID, Scope: KVScopeNode, Key: m.Key,
				Value: m.Value, ExpiresAt: expiresAt, UpdatedAt: now,
			}); err != nil {
				return fmt.Errorf("writing node kv %s/%s: %w", m.NodeID, m.Key, err)
			}
		}

		for _, nr := range b.NodeRuns {
			var errCol sql.NullString
			if nr.Err != "" {
				errCol = sql.NullString{String: nr.Err, Valid: true}
			}
			if err := tx.queries.InsertNodeRun(ctx, InsertNodeRunParams{
				FlowID:    nr.FlowID,
				NodeID:    nr.NodeID,
				Ok:        boolToInt64(nr.OK),
				InCount:   int64(nr.InCount),
				OutCount:  int64(nr.OutCount),
				DropCount: int64(nr.DropCount),
				Err:       errCol,
				EndedAt:   now,
				DurMs:     nr.DurMs,
			}); err != nil {
				return fmt.Errorf("inserting node_run for %s/%s: %w", nr.FlowID, nr.NodeID, err)
			}
		}

		if err := tx.queries.CommitConsumerOffset(ctx, CommitConsumerOffsetParams{
			Consumer: b.Consumer,
			Offset:   b.UpToOffset,
		}); err != nil {
			return fmt.Errorf("advancing consumer offset for %q: %w", b.Consumer, err)
		}

		return nil
	})
}

// notifyDedupKey is the output_command dedup key for a notify output. The
// classifier's occurrence key is the right one whenever it exists: it changes
// exactly when something meaningful changed about the item, so a source that
// re-emits an unchanged item on every poll notifies once, not once per tick.
//
// Not every event carries one — a trivial update, or a message a function
// node synthesized, may have none — and falling back to the empty string
// would make (action_id, "") unique for the node forever, i.e. it would
// notify exactly once and then go permanently silent. The fallback is
// therefore the item plus a digest of its payload: distinct payloads still
// notify, identical ones still deduplicate.
func notifyDedupKey(out Output) string {
	if out.OccurrenceKey != "" {
		return out.OccurrenceKey
	}
	sum := sha256.Sum256(out.Payload)
	return out.Key + "@" + hex.EncodeToString(sum[:8])
}

// mintFeedInboxItem creates the durable row behind a feed output whose key
// never went through ingest — a function node minted it while splitting one
// source message into per-entity items. Presentation comes from the payload
// (title/url), the same fields the producer reads at the ingest boundary; the
// lifecycle is active because the item is present in the snapshot that carried
// it, and its absence from a later snapshot drops the membership claim rather
// than archiving the row. A subsequent ingest under the same identity upserts
// this row in place, so a genuine source item briefly missing at commit
// self-heals rather than forking a duplicate.
func mintFeedInboxItem(ctx context.Context, q *Queries, profileID string, out Output, now int64) (InboxItem, error) {
	title, url := feedItemPresentation(out.Key, out.Payload)
	return q.InsertInboxItem(ctx, InsertInboxItemParams{
		ProfileID:   profileID,
		SourceKind:  out.SourceKind,
		SourceScope: out.SourceScope,
		ExternalID:  out.Key,
		Title:       title,
		Url:         url,
		Payload:     out.Payload,
		Unread:      1,
		Lifecycle:   LifecycleActive.String(),
		FirstSeenAt: now,
		LastEventAt: now,
	})
}

// feedItemPresentation reads the title and url a synthesized feed item renders
// with from its payload, mirroring the ingest boundary's convention. A payload
// with no title falls back to the key, so an item is never blank.
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

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
