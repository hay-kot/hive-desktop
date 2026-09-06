package stores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// EventLogStore owns event_log and consumer_offset: the append-only pipeline
// log and each consumer's read checkpoint into it. Commit and ActivateReplay
// -- the operations that write other aggregates inside this one's
// transaction -- land here in phase 3b; this store is the leaf half.
type EventLogStore struct {
	q   *queries.DB
	now func() time.Time
}

func NewEventLogStore(q *queries.DB, opts Options) *EventLogStore {
	return &EventLogStore{q: q, now: opts.Now}
}

// Append inserts a new event_log row under topic, keyed by key, and returns
// its offset.
func (s *EventLogStore) Append(ctx context.Context, topic, key string, payload []byte) (int64, error) {
	offset, err := s.q.Ctx(ctx).AppendEvent(ctx, queries.AppendEventParams{
		Topic:      topic,
		Key:        key,
		Payload:    payload,
		CreatedAt:  s.now().UnixMilli(),
		Snapshot:   0,
		SourceKind: "", SourceScope: "", OccurrenceKey: sql.NullString{},
	})
	return offset, wrap(fmt.Sprintf("appending event to topic %q", topic), err)
}

// AppendSnapshot appends a successful source poll's complete current item
// set. Unlike item events, snapshots are deliberately not deduplicated: each
// one is an authoritative reconciliation point, including an empty set.
func (s *EventLogStore) AppendSnapshot(ctx context.Context, topic, sourceKind, sourceScope string, items []models.SnapshotItem) (int64, error) {
	payload, err := json.Marshal(items)
	if err != nil {
		return 0, fmt.Errorf("encoding source snapshot for topic %q: %w", topic, err)
	}
	offset, err := s.q.Ctx(ctx).AppendEvent(ctx, queries.AppendEventParams{
		Topic:      topic,
		Key:        "",
		Payload:    payload,
		CreatedAt:  s.now().UnixMilli(),
		Snapshot:   1,
		SourceKind: sourceKind, SourceScope: sourceScope, OccurrenceKey: sql.NullString{},
	})
	return offset, wrap(fmt.Sprintf("appending source snapshot for topic %q", topic), err)
}

// ReadFrom returns up to limit event_log rows with offset > offset, ordered
// ascending, along with the offset of the last row returned (nextOffset). If
// no rows are found, nextOffset is the offset argument unchanged, so callers
// can always resume with ReadFrom(ctx, nextOffset, limit).
func (s *EventLogStore) ReadFrom(ctx context.Context, offset int64, limit int) ([]models.Msg, int64, error) {
	rows, err := s.q.Ctx(ctx).ReadEventsFrom(ctx, queries.ReadEventsFromParams{
		Offset: offset,
		Limit:  int64(limit),
	})
	if err != nil {
		return nil, offset, fmt.Errorf("reading events from offset %d: %w", offset, err)
	}

	msgs := make([]models.Msg, 0, len(rows))
	nextOffset := offset
	for _, row := range rows {
		msg := models.Msg{
			ID:            strconv.FormatInt(row.Offset, 10),
			Key:           row.Key,
			Topic:         row.Topic,
			Ts:            row.CreatedAt,
			Payload:       json.RawMessage(row.Payload),
			SourceKind:    row.SourceKind,
			SourceScope:   row.SourceScope,
			OccurrenceKey: row.OccurrenceKey.String,
		}
		if row.Snapshot != 0 {
			if err := json.Unmarshal(row.Payload, &msg.Snapshot); err != nil {
				return nil, offset, fmt.Errorf("decoding source snapshot at offset %d: %w", row.Offset, err)
			}
		}
		msgs = append(msgs, msg)
		nextOffset = row.Offset
	}

	return msgs, nextOffset, nil
}

// ReadForConsumer returns up to limit events after consumer's persisted
// checkpoint. Consumers therefore resume from their last successful commit,
// including after the frontend runtime restarts.
func (s *EventLogStore) ReadForConsumer(ctx context.Context, consumer string, limit int) ([]models.Msg, error) {
	offset, err := s.ConsumerOffset(ctx, consumer)
	if err != nil {
		return nil, err
	}
	msgs, _, err := s.ReadFrom(ctx, offset, limit)
	return msgs, err
}

// ConsumerOffset returns the last offset committed by consumer, or 0 if the
// consumer has never committed.
func (s *EventLogStore) ConsumerOffset(ctx context.Context, consumer string) (int64, error) {
	row, err := s.q.Ctx(ctx).GetConsumerOffset(ctx, consumer)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("reading committed offset for consumer %q: %w", consumer, err)
	}
	return row.Offset, nil
}

// TailOffset returns the AUTOINCREMENT high-water mark. Unlike MAX(offset),
// it remains stable after event-log retention deletes rows.
func (s *EventLogStore) TailOffset(ctx context.Context) (int64, error) {
	tail, err := s.q.Ctx(ctx).GetEventLogTailOffset(ctx)
	return tail, wrap("getting event log tail", err)
}

// ListLatestSnapshots returns each profile source's newest authoritative
// snapshot at or before throughOffset. Keeping the source topic on each
// message preserves provenance when a deployed graph recomputes feed
// memberships.
func (s *EventLogStore) ListLatestSnapshots(ctx context.Context, profileID string, throughOffset int64) ([]models.Msg, error) {
	if throughOffset < 0 {
		return nil, fmt.Errorf("listing replay source snapshots for %q: negative offset", profileID)
	}
	prefix := "source:" + profileID + "/"
	rows, err := s.q.Ctx(ctx).ListLatestSourceSnapshotsByTopicPrefix(ctx, queries.ListLatestSourceSnapshotsByTopicPrefixParams{ThroughOffset: throughOffset, TopicPrefix: prefix})
	if err != nil {
		return nil, fmt.Errorf("listing replay source snapshots for %q: %w", profileID, err)
	}

	messages := make([]models.Msg, 0, len(rows))
	for _, row := range rows {
		var snapshot []models.SnapshotItem
		if err := json.Unmarshal(row.Payload, &snapshot); err != nil {
			return nil, fmt.Errorf("decoding replay source snapshot at offset %d: %w", row.Offset, err)
		}
		messages = append(messages, models.Msg{
			ID:          strconv.FormatInt(row.Offset, 10),
			Topic:       row.Topic,
			Ts:          row.CreatedAt,
			Payload:     json.RawMessage(row.Payload),
			Snapshot:    snapshot,
			SourceKind:  row.SourceKind,
			SourceScope: row.SourceScope,
		})
	}
	return messages, nil
}

// DeleteByTopicPrefix removes every event_log row under prefix. Used by
// FlowsService.PurgeProfile (3b) to erase a deleted profile's log entries.
func (s *EventLogStore) DeleteByTopicPrefix(ctx context.Context, prefix string) error {
	return wrap("deleting event log by topic prefix", s.q.Ctx(ctx).DeleteEventLogByTopicPrefix(ctx, prefix))
}
