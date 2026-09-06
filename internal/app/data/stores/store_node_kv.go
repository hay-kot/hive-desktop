package stores

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// KVScopeNode is the only scope exposed today; the node_kv scope column
// reserves room for future "flow"/"global" scopes without a schema change.
// No method takes a scope parameter -- each hardcodes this constant -- so a
// scope argument appears on the API only when a second scope actually ships.
const KVScopeNode = "node"

// NodeKVStore owns node_kv: the durable key/value scratch space a function
// node reads and writes across ticks.
type NodeKVStore struct {
	q   *queries.DB
	now func() time.Time
}

func NewNodeKVStore(q *queries.DB, opts Options) *NodeKVStore {
	return &NodeKVStore{q: q, now: opts.Now}
}

// Get reads one key's stored JSON text. The caller supplies the `now` cutoff
// (unix ms) so expiry is deterministic: a row with expires_at <= now reads
// as absent, and the runtime buffer can pin one cutoff per tick.
func (s *NodeKVStore) Get(ctx context.Context, flowID, nodeID, key string, now int64) (string, bool, error) {
	value, err := s.q.Ctx(ctx).GetNodeKV(ctx, queries.GetNodeKVParams{
		FlowID: flowID, NodeID: nodeID, Scope: KVScopeNode, Key: key, ExpiresAt: sql.NullInt64{Int64: now, Valid: true},
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, wrap("reading node kv", err)
	}
	return value, true, nil
}

// Keys lists the unexpired keys under one node that start with prefix,
// sorted. The match is binary/case-sensitive (GLOB), agreeing exactly with
// strings.HasPrefix so a durable read and the runtime's in-tick overlay can
// never disagree on what a prefix covers.
func (s *NodeKVStore) Keys(ctx context.Context, flowID, nodeID, prefix string, now int64) ([]string, error) {
	keys, err := s.q.Ctx(ctx).ListNodeKVKeysByPrefix(ctx, queries.ListNodeKVKeysByPrefixParams{
		FlowID: flowID, NodeID: nodeID, Scope: KVScopeNode,
		Pattern: escapeGlob(prefix) + "*",
		Now:     sql.NullInt64{Int64: now, Valid: true},
	})
	return keys, wrap("listing node kv keys", err)
}

// Set upserts one key. value is opaque JSON text -- (de)serialization
// belongs to the caller. expiresAt is unix ms; 0 means no expiry.
func (s *NodeKVStore) Set(ctx context.Context, flowID, nodeID, key, value string, expiresAt int64) error {
	var exp sql.NullInt64
	if expiresAt > 0 {
		exp = sql.NullInt64{Int64: expiresAt, Valid: true}
	}
	return wrap("writing node kv", s.q.Ctx(ctx).UpsertNodeKV(ctx, queries.UpsertNodeKVParams{
		FlowID: flowID, NodeID: nodeID, Scope: KVScopeNode, Key: key,
		Value: value, ExpiresAt: exp, UpdatedAt: s.now().UnixMilli(),
	}))
}

// Delete removes one key. Used by EventLogStore.Commit for a KVMutation
// marked Delete.
func (s *NodeKVStore) Delete(ctx context.Context, flowID, nodeID, key string) error {
	return wrap("deleting node kv", s.q.Ctx(ctx).DeleteNodeKV(ctx, queries.DeleteNodeKVParams{
		FlowID: flowID, NodeID: nodeID, Scope: KVScopeNode, Key: key,
	}))
}

// DeleteByFlow removes every node_kv row for flowID. Used by
// FlowsService.purgeProfile when a workspace is deleted, and by
// EventLogStore.ActivateReplay when a flow has no KV-capable nodes left.
func (s *NodeKVStore) DeleteByFlow(ctx context.Context, flowID string) error {
	return wrap("deleting node kv by flow", s.q.Ctx(ctx).DeleteNodeKVByFlow(ctx, flowID))
}

// DeleteForFlowExceptNodes removes flowID's node kv except rows owned by
// keepNodeIDs. Used by EventLogStore.ActivateReplay to reconcile a flow's KV
// against the node ids that survived a reload.
func (s *NodeKVStore) DeleteForFlowExceptNodes(ctx context.Context, flowID string, keepNodeIDs []string) error {
	return wrap("removing obsolete node kv", s.q.Ctx(ctx).DeleteNodeKVForFlowExceptNodes(ctx, queries.DeleteNodeKVForFlowExceptNodesParams{
		FlowID: flowID, NodeIds: keepNodeIDs,
	}))
}

// escapeGlob neutralizes GLOB metacharacters so a stored key's literal
// prefix always matches itself. `*` and `?` wrap in a character class; `[`
// is escaped the only way GLOB allows -- opening a class that contains it.
func escapeGlob(s string) string {
	replacer := strings.NewReplacer("*", "[*]", "?", "[?]", "[", "[[]")
	return replacer.Replace(s)
}
