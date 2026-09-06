package queries

import (
	"context"
	"fmt"
)

// SourceIdentity scopes an active source head listing to the inbox rows one
// connector instance owns.
type SourceIdentity struct {
	Topic       string
	ProfileID   string
	SourceKind  string
	SourceScope string
}

// ListActiveSourceHeadKeys returns membership for one source topic, limited
// to keys whose inbox row still exists and is not archived.
func (db *DB) ListActiveSourceHeadKeys(ctx context.Context, id SourceIdentity) ([]string, error) {
	keys, err := db.Queries.ListActiveSourceHeadKeys(ctx, ListActiveSourceHeadKeysParams{
		Topic:       id.Topic,
		ProfileID:   id.ProfileID,
		SourceKind:  id.SourceKind,
		SourceScope: id.SourceScope,
	})
	if err != nil {
		return nil, fmt.Errorf("listing active source head keys: %w", err)
	}
	return keys, nil
}

// SourceHeadPayload returns one topic-scoped source membership payload.
func (db *DB) SourceHeadPayload(ctx context.Context, topic, key string) ([]byte, error) {
	payload, err := db.GetSourceHeadPayload(ctx, GetSourceHeadPayloadParams{Topic: topic, Key: key})
	if err != nil {
		return nil, fmt.Errorf("getting source head payload: %w", err)
	}
	return payload, nil
}

// DeleteSourceHead evicts one topic-scoped source head row.
func (db *DB) DeleteSourceHead(ctx context.Context, topic, key string) error {
	if err := db.Queries.DeleteSourceHead(ctx, DeleteSourceHeadParams{Topic: topic, Key: key}); err != nil {
		return fmt.Errorf("deleting source head: %w", err)
	}
	return nil
}
