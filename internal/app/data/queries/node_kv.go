package queries

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// KVScopeNode is the only scope exposed today; the node_kv scope column
// reserves room for future "flow"/"global" scopes without a schema change.
// No wrapper takes a scope parameter — each hardcodes this constant — so a
// scope argument appears on the API only when a second scope actually ships.
const KVScopeNode = "node"

// NodeKVGet reads one key's stored JSON text. The caller supplies the `now`
// cutoff (unix ms) so expiry is deterministic: a row with expires_at <= now
// reads as absent, and the runtime buffer can pin one cutoff per tick.
func (db *DB) NodeKVGet(ctx context.Context, flowID, nodeID, key string, now int64) (string, bool, error) {
	value, err := db.GetNodeKV(ctx, GetNodeKVParams{
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

// NodeKVKeys lists the unexpired keys under one node that start with prefix,
// sorted. The match is binary/case-sensitive (GLOB), agreeing exactly with
// strings.HasPrefix so a durable read and the runtime's in-tick overlay can
// never disagree on what a prefix covers.
func (db *DB) NodeKVKeys(ctx context.Context, flowID, nodeID, prefix string, now int64) ([]string, error) {
	keys, err := db.ListNodeKVKeysByPrefix(ctx, ListNodeKVKeysByPrefixParams{
		FlowID: flowID, NodeID: nodeID, Scope: KVScopeNode,
		Pattern: escapeGlob(prefix) + "*",
		Now:     sql.NullInt64{Int64: now, Valid: true},
	})
	return keys, wrap("listing node kv keys", err)
}

// NodeKVSet upserts one key. value is opaque JSON text — (de)serialization
// belongs to the caller. expiresAt is unix ms; 0 means no expiry.
func (db *DB) NodeKVSet(ctx context.Context, flowID, nodeID, key, value string, expiresAt int64) error {
	var exp sql.NullInt64
	if expiresAt > 0 {
		exp = sql.NullInt64{Int64: expiresAt, Valid: true}
	}
	return wrap("writing node kv", db.UpsertNodeKV(ctx, UpsertNodeKVParams{
		FlowID: flowID, NodeID: nodeID, Scope: KVScopeNode, Key: key,
		Value: value, ExpiresAt: exp, UpdatedAt: time.Now().UnixMilli(),
	}))
}

// escapeGlob neutralizes GLOB metacharacters so a stored key's literal
// prefix always matches itself. `*` and `?` wrap in a character class; `[`
// is escaped the only way GLOB allows — opening a class that contains it.
func escapeGlob(s string) string {
	replacer := strings.NewReplacer("*", "[*]", "?", "[?]", "[", "[[]")
	return replacer.Replace(s)
}
