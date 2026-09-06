package queries

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
)

// AppendSnapshot is the Queries-level form of DB.AppendSnapshot, so
// transactional callers can append a snapshot atomically with other
// writes.
func (q *Queries) AppendSnapshot(ctx context.Context, topic, sourceKind, sourceScope string, items []models.SnapshotItem) (int64, error) {
	payload, err := json.Marshal(items)
	if err != nil {
		return 0, fmt.Errorf("encoding source snapshot for topic %q: %w", topic, err)
	}
	offset, err := q.AppendEvent(ctx, AppendEventParams{
		Topic:      topic,
		Key:        "",
		Payload:    payload,
		CreatedAt:  time.Now().UnixMilli(),
		Snapshot:   1,
		SourceKind: sourceKind, SourceScope: sourceScope, OccurrenceKey: sql.NullString{},
	})
	if err != nil {
		return 0, fmt.Errorf("appending source snapshot for topic %q: %w", topic, err)
	}
	return offset, nil
}

const getEventLogTailOffset = `
SELECT CAST(COALESCE((SELECT seq FROM sqlite_sequence WHERE name = 'event_log'), 0) AS INTEGER)
`

// GetEventLogTailOffset returns the AUTOINCREMENT high-water mark. Unlike
// MAX(offset), it remains stable after event-log retention deletes rows.
func (q *Queries) GetEventLogTailOffset(ctx context.Context) (int64, error) {
	var tail int64
	err := q.db.QueryRowContext(ctx, getEventLogTailOffset).Scan(&tail)
	return tail, err
}

const trimInboxItemEvents = `
DELETE FROM inbox_event
WHERE id IN (
    SELECT id FROM (
        SELECT id, ROW_NUMBER() OVER (PARTITION BY item_id ORDER BY id DESC) AS rn
        FROM inbox_event
    ) WHERE rn > ?
)
`

// TrimInboxItemEvents keeps the newest limit events for every inbox item.
func (q *Queries) TrimInboxItemEvents(ctx context.Context, limit int64) error {
	_, err := q.db.ExecContext(ctx, trimInboxItemEvents, limit)
	return err
}
