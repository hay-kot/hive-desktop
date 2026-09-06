package queries

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hay-kot/hive-desktop/internal/app/data/models"
)

// enqueueTestCommand commits a single action output through the production
// boundary, the fixture several tests in this package share to get a
// runnable output_command row.
func enqueueTestCommand(t *testing.T, db *DB, actionID, key string) {
	t.Helper()
	require.NoError(t, db.CommitBatch(t.Context(), models.CommitBatch{
		Consumer:   "flow-" + actionID + "-" + key,
		UpToOffset: 1,
		Outputs: []models.Output{
			{
				Sink:          models.Sink{Kind: models.SinkKindAction, TargetID: actionID},
				OccurrenceKey: key,
				Payload:       []byte(`{"v":1}`),
			},
		},
	}))
}

// This file gives commit_test.go, replay_test.go, retention_test.go,
// dbext_test.go and timestamp_test.go back the thin wrappers phase 3a moved
// onto stores.EventLogStore, stores.NodeKVStore, stores.OutputCommandStore
// and stores.NodeRunStore. CommitBatch, ActivateReplay and PurgeProfile stay
// *DB methods here until phase 3b, and their tests exercise the tables those
// operations touch through these test-only equivalents rather than
// hand-rolling the same generated-query calls the real stores also wrap.
// Production code never sees this file.

func (db *DB) Append(ctx context.Context, topic, key string, payload []byte) (int64, error) {
	return db.AppendEvent(ctx, AppendEventParams{
		Topic: topic, Key: key, Payload: payload, CreatedAt: unixMilliNow(),
		Snapshot: 0, SourceKind: "", SourceScope: "", OccurrenceKey: sql.NullString{},
	})
}

func (db *DB) AppendSnapshot(ctx context.Context, topic, sourceKind, sourceScope string, items []models.SnapshotItem) (int64, error) {
	payload, err := json.Marshal(items)
	if err != nil {
		return 0, fmt.Errorf("encoding source snapshot for topic %q: %w", topic, err)
	}
	return db.AppendEvent(ctx, AppendEventParams{
		Topic: topic, Key: "", Payload: payload, CreatedAt: unixMilliNow(),
		Snapshot: 1, SourceKind: sourceKind, SourceScope: sourceScope, OccurrenceKey: sql.NullString{},
	})
}

func (db *DB) ReadFrom(ctx context.Context, offset int64, limit int) ([]models.Msg, int64, error) {
	rows, err := db.ReadEventsFrom(ctx, ReadEventsFromParams{Offset: offset, Limit: int64(limit)})
	if err != nil {
		return nil, offset, fmt.Errorf("reading events from offset %d: %w", offset, err)
	}
	msgs := make([]models.Msg, 0, len(rows))
	nextOffset := offset
	for _, row := range rows {
		msg := models.Msg{
			ID: strconv.FormatInt(row.Offset, 10), Key: row.Key, Topic: row.Topic, Ts: row.CreatedAt,
			Payload: json.RawMessage(row.Payload), SourceKind: row.SourceKind, SourceScope: row.SourceScope,
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

func (db *DB) EventLogTailOffset(ctx context.Context) (int64, error) {
	return db.GetEventLogTailOffset(ctx)
}

func (db *DB) ListUnarchivedInboxItems(ctx context.Context, profileID string) ([]InboxItem, error) {
	return db.ListUnarchivedInboxItemsByProfile(ctx, profileID)
}

func (db *DB) ListReplaySourceSnapshots(ctx context.Context, profileID string, throughOffset int64) ([]models.Msg, error) {
	prefix := "source:" + profileID + "/"
	rows, err := db.ListLatestSourceSnapshotsByTopicPrefix(ctx, ListLatestSourceSnapshotsByTopicPrefixParams{ThroughOffset: throughOffset, TopicPrefix: prefix})
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
			ID: strconv.FormatInt(row.Offset, 10), Topic: row.Topic, Ts: row.CreatedAt,
			Payload: json.RawMessage(row.Payload), Snapshot: snapshot, SourceKind: row.SourceKind, SourceScope: row.SourceScope,
		})
	}
	return messages, nil
}

func (db *DB) NodeKVGet(ctx context.Context, flowID, nodeID, key string, now int64) (string, bool, error) {
	value, err := db.GetNodeKV(ctx, GetNodeKVParams{
		FlowID: flowID, NodeID: nodeID, Scope: KVScopeNode, Key: key, ExpiresAt: sql.NullInt64{Int64: now, Valid: true},
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

func (db *DB) NodeKVSet(ctx context.Context, flowID, nodeID, key, value string, expiresAt int64) error {
	var exp sql.NullInt64
	if expiresAt > 0 {
		exp = sql.NullInt64{Int64: expiresAt, Valid: true}
	}
	return db.UpsertNodeKV(ctx, UpsertNodeKVParams{
		FlowID: flowID, NodeID: nodeID, Scope: KVScopeNode, Key: key, Value: value, ExpiresAt: exp, UpdatedAt: unixMilliNow(),
	})
}

func (db *DB) ConfirmOutputCommand(ctx context.Context, actionID, key string, payload []byte, ref models.ItemRef) (OutputCommand, bool, error) {
	row, err := db.Queries.ConfirmOutputCommand(ctx, ConfirmOutputCommandParams{
		ActionID: actionID, Key: key, Payload: payload, CreatedAt: unixMilliNow(),
		ProfileID: ref.ProfileID, SourceKind: ref.SourceKind, SourceScope: ref.SourceScope, ExternalID: ref.ExternalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		existing, lookupErr := db.GetLatestOutputCommandForAction(ctx, GetLatestOutputCommandForActionParams{ActionID: actionID, Key: key})
		return existing, false, lookupErr
	}
	return row, err == nil, err
}

func (db *DB) OutputCommand(ctx context.Context, id int64) (OutputCommand, error) {
	return db.GetOutputCommand(ctx, id)
}

func (db *DB) ListRunnableOutputCommandsAfter(ctx context.Context, afterID int64, limit int) ([]OutputCommand, error) {
	return db.Queries.ListRunnableOutputCommandsAfter(ctx, ListRunnableOutputCommandsAfterParams{ID: afterID, Limit: int64(limit)})
}

func (db *DB) NodeRuns(ctx context.Context, flowID string, limit int) ([]NodeRun, error) {
	return db.ListNodeRunsByFlow(ctx, ListNodeRunsByFlowParams{FlowID: flowID, Limit: int64(limit)})
}

func unixMilliNow() int64 { return time.Now().UnixMilli() }
