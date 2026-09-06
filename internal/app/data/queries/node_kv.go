package queries

// KVScopeNode is the only scope exposed today; the node_kv scope column
// reserves room for future "flow"/"global" scopes without a schema change.
// This package's own test fixtures (NodeKVSet) are its only remaining
// callers; stores.KVScopeNode is the copy every NodeKVStore method uses.
const KVScopeNode = "node"
