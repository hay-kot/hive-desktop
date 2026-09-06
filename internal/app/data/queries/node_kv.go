package queries

// KVScopeNode is the only scope exposed today; the node_kv scope column
// reserves room for future "flow"/"global" scopes without a schema change.
// CommitBatch (still a *DB method until phase 3b moves it onto
// EventLogStore) is this constant's one remaining caller here;
// stores.KVScopeNode is the copy every NodeKVStore method uses.
const KVScopeNode = "node"
