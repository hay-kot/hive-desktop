package queries

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/observe"
)

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
func (db *DB) CommitBatch(ctx context.Context, b models.CommitBatch) error {
	ctx, span := observe.StartConditionalSpan(ctx, tracer, "db.CommitBatch")
	defer span.End()
	span.SetAttributes(attribute.Int("db.batch.outputs", len(b.Outputs)))

	db.debugPauseCommit(ctx)
	if b.UpToOffset < 0 {
		return fmt.Errorf("commit offset must not be negative: %d", b.UpToOffset)
	}

	return db.WithinTx(ctx, func(ctx context.Context, tx *DB) error {
		current, err := tx.GetConsumerOffset(ctx, b.Consumer)
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
			case models.SinkKindFeed:
				item, err := resolveInboxItemScoped(ctx, tx.Queries, b.Consumer, out.SourceKind, out.SourceScope, out.Key)
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
					item, err = mintFeedInboxItem(ctx, tx.Queries, b.Consumer, out, now)
					if err != nil {
						return fmt.Errorf("minting inbox item %s/%s/%s: %w", out.SourceKind, out.SourceScope, out.Key, err)
					}
				} else if err != nil {
					return fmt.Errorf("resolving inbox item %s/%s/%s: %w", out.SourceKind, out.SourceScope, out.Key, err)
				}
				if err := tx.UpsertFeedMembershipClaim(ctx, UpsertFeedMembershipClaimParams{
					ProfileID: b.Consumer, FeedID: out.Sink.TargetID, ItemID: item.ID, SourceID: out.SourceTopic,
				}); err != nil {
					return fmt.Errorf("claiming feed membership %s/%s: %w", out.Sink.TargetID, out.Key, err)
				}
			case models.SinkKindAction:
				// The dedup key is the occurrence key, so the row cannot be
				// traced back to its item by key alone.
				if err := tx.EnqueueOutputCommand(ctx, EnqueueOutputCommandParams{
					ActionID: out.Sink.TargetID, Key: out.OccurrenceKey, Payload: []byte(out.Payload), CreatedAt: now,
					ProfileID: b.Consumer, SourceKind: out.SourceKind, SourceScope: out.SourceScope, ExternalID: out.Key,
				}); err != nil {
					return fmt.Errorf("enqueuing output_command %s/%s: %w", out.Sink.TargetID, out.OccurrenceKey, err)
				}
			case models.SinkKindNotify:
				payload, err := json.Marshal(models.NotifyCommand{
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
				if err := tx.EnqueueOutputCommand(ctx, EnqueueOutputCommandParams{
					ActionID: models.NotifyActionID(out.Sink.TargetID), Key: key, Payload: payload, CreatedAt: now,
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
				if out.Sink.Kind != models.SinkKindFeed || out.Sink.TargetID != snapshot.FeedID || out.SourceTopic != snapshot.SourceTopic || out.SnapshotID != snapshot.SnapshotID {
					continue
				}
				item, err := resolveInboxItemScoped(ctx, tx.Queries, b.Consumer, out.SourceKind, out.SourceScope, out.Key)
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
				if err := tx.DeleteFeedMembershipClaimsForSourceAll(ctx, DeleteFeedMembershipClaimsForSourceAllParams{FeedID: snapshot.FeedID, SourceID: snapshot.SourceTopic}); err != nil {
					return fmt.Errorf("clearing empty feed snapshot: %w", err)
				}
			} else if err := tx.DeleteFeedMembershipClaimsNotInSnapshot(ctx, DeleteFeedMembershipClaimsNotInSnapshotParams{FeedID: snapshot.FeedID, SourceID: snapshot.SourceTopic, ItemIds: itemIDs}); err != nil {
				return fmt.Errorf("reconciling feed snapshot: %w", err)
			}
		}

		for _, m := range b.KVMutations {
			if m.Delete {
				if err := tx.DeleteNodeKV(ctx, DeleteNodeKVParams{FlowID: b.Consumer, NodeID: m.NodeID, Scope: KVScopeNode, Key: m.Key}); err != nil {
					return fmt.Errorf("deleting node kv %s/%s: %w", m.NodeID, m.Key, err)
				}
				continue
			}
			var expiresAt sql.NullInt64
			if m.ExpiresAt > 0 {
				expiresAt = sql.NullInt64{Int64: m.ExpiresAt, Valid: true}
			}
			if err := tx.UpsertNodeKV(ctx, UpsertNodeKVParams{
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
			if err := tx.InsertNodeRun(ctx, InsertNodeRunParams{
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

		if err := tx.CommitConsumerOffset(ctx, CommitConsumerOffsetParams{
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
func notifyDedupKey(out models.Output) string {
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
func mintFeedInboxItem(ctx context.Context, q *Queries, profileID string, out models.Output, now int64) (InboxItem, error) {
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
		Lifecycle:   models.LifecycleActive.String(),
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
