package queries

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
)

// Append inserts a new event_log row under topic, keyed by key, and returns
// its offset. created_at is stamped as the current unix millisecond time.
func (db *DB) Append(ctx context.Context, topic, key string, payload []byte) (int64, error) {
	offset, err := db.AppendEvent(ctx, AppendEventParams{
		Topic:      topic,
		Key:        key,
		Payload:    payload,
		CreatedAt:  time.Now().UnixMilli(),
		Snapshot:   0,
		SourceKind: "", SourceScope: "", OccurrenceKey: sql.NullString{},
	})
	if err != nil {
		return 0, fmt.Errorf("appending event to topic %q: %w", topic, err)
	}
	return offset, nil
}

// AppendSnapshot appends a successful source poll's complete current item
// set. Unlike item events, snapshots are deliberately not deduplicated: each
// one is an authoritative reconciliation point, including an empty set.
func (db *DB) AppendSnapshot(ctx context.Context, topic, sourceKind, sourceScope string, items []models.SnapshotItem) (int64, error) {
	return db.Queries.AppendSnapshot(ctx, topic, sourceKind, sourceScope, items)
}

// ReadFrom returns up to limit event_log rows with offset > offset, ordered
// ascending, along with the offset of the last row returned (nextOffset).
// If no rows are found, nextOffset is the offset argument unchanged, so
// callers can always resume with ReadFrom(ctx, nextOffset, limit).
func (db *DB) ReadFrom(ctx context.Context, offset int64, limit int) ([]models.Msg, int64, error) {
	rows, err := db.ReadEventsFrom(ctx, ReadEventsFromParams{
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
func (db *DB) ReadForConsumer(ctx context.Context, consumer string, limit int) ([]models.Msg, error) {
	offset, err := db.ConsumerOffset(ctx, consumer)
	if err != nil {
		return nil, err
	}
	msgs, _, err := db.ReadFrom(ctx, offset, limit)
	return msgs, err
}

// ConsumerOffset returns the last offset committed by consumer, or 0 if the
// consumer has never committed.
func (db *DB) ConsumerOffset(ctx context.Context, consumer string) (int64, error) {
	row, err := db.GetConsumerOffset(ctx, consumer)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("reading committed offset for consumer %q: %w", consumer, err)
	}
	return row.Offset, nil
}
