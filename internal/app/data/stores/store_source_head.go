package stores

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// SourceHeadStore owns source_head: the last-seen payload behind each
// (topic, key), used to detect an unchanged observation and to drive absence
// confirmation.
type SourceHeadStore struct {
	q *queries.DB
}

func NewSourceHeadStore(q *queries.DB, _ Options) *SourceHeadStore {
	return &SourceHeadStore{q: q}
}

// ListActiveKeys returns membership for one source topic, limited to keys
// whose inbox row still exists and is not archived.
func (s *SourceHeadStore) ListActiveKeys(ctx context.Context, id SourceIdentity) ([]string, error) {
	keys, err := s.q.Ctx(ctx).ListActiveSourceHeadKeys(ctx, queries.ListActiveSourceHeadKeysParams{
		Topic:       id.Topic,
		ProfileID:   id.ProfileID,
		SourceKind:  id.SourceKind,
		SourceScope: id.SourceScope,
	})
	return keys, wrap("listing active source head keys", err)
}

// Payload returns one topic-scoped source membership payload.
func (s *SourceHeadStore) Payload(ctx context.Context, topic, key string) ([]byte, error) {
	payload, err := s.q.Ctx(ctx).GetSourceHeadPayload(ctx, queries.GetSourceHeadPayloadParams{Topic: topic, Key: key})
	return payload, wrap("getting source head payload", err)
}

// Upsert records the latest payload seen for (topic, key).
func (s *SourceHeadStore) Upsert(ctx context.Context, topic, key string, payload []byte) error {
	return wrap("upserting source head", s.q.Ctx(ctx).UpsertSourceHead(ctx, queries.UpsertSourceHeadParams{
		Topic: topic, Key: key, Payload: payload,
	}))
}

// Delete evicts one topic-scoped source head row.
func (s *SourceHeadStore) Delete(ctx context.Context, topic, key string) error {
	return wrap("deleting source head", s.q.Ctx(ctx).DeleteSourceHead(ctx, queries.DeleteSourceHeadParams{Topic: topic, Key: key}))
}

// DeleteByTopicPrefix removes every source_head row under prefix. It is the
// per-store half of FlowsService.PurgeProfile (3b): a profile's topics are
// escaped-LIKE prefixed, so the caller supplies an already-escaped prefix.
func (s *SourceHeadStore) DeleteByTopicPrefix(ctx context.Context, prefix string) error {
	return wrap("deleting source heads by topic prefix", s.q.Ctx(ctx).DeleteSourceHeadByTopicPrefix(ctx, prefix))
}
