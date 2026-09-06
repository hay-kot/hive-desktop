package stores

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStores_TxJoinsTwoStoresAndCommits proves Tx is the one cross-aggregate
// entry point a service is allowed to use: two different stores write inside
// the same transaction and both writes land together.
func TestStores_TxJoinsTwoStoresAndCommits(t *testing.T) {
	st, db := openTestStores(t)
	ctx := t.Context()

	txCtx, tx, err := st.Tx(ctx)
	require.NoError(t, err)

	require.NoError(t, st.NodeKV.Set(txCtx, "flow-1", "node-a", "k", "v", 0))
	require.NoError(t, st.WebhookCaptures.Upsert(txCtx, "source:flow-1/hook", 100, []byte(`{}`)))

	require.Equal(t, 1, db.Conn().Stats().InUse, "both writes must share one connection")
	require.NoError(t, tx.Commit())

	_, found, err := st.NodeKV.Get(ctx, "flow-1", "node-a", "k", 1)
	require.NoError(t, err)
	assert.True(t, found)
	_, err = st.WebhookCaptures.Get(ctx, "source:flow-1/hook")
	require.NoError(t, err)
}

// TestStores_TxRollbackUndoesBothWrites is the other half: a rollback after
// two stores wrote inside the same Tx must leave neither write behind.
func TestStores_TxRollbackUndoesBothWrites(t *testing.T) {
	st, _ := openTestStores(t)
	ctx := t.Context()

	txCtx, tx, err := st.Tx(ctx)
	require.NoError(t, err)

	require.NoError(t, st.NodeKV.Set(txCtx, "flow-1", "node-a", "k", "v", 0))
	require.NoError(t, st.WebhookCaptures.Upsert(txCtx, "source:flow-1/hook", 100, []byte(`{}`)))

	require.NoError(t, tx.Rollback())

	_, found, err := st.NodeKV.Get(ctx, "flow-1", "node-a", "k", 1)
	require.NoError(t, err)
	assert.False(t, found, "the rollback must undo the node kv write")
	_, err = st.WebhookCaptures.Get(ctx, "source:flow-1/hook")
	assert.True(t, IsNotFound(err), "the rollback must undo the webhook capture write")
}

// TestStores_TxJoinsAnAlreadyOpenTransaction proves Tx joins an ambient
// transaction rather than nesting a second one, which would deadlock under
// this database's _txlock=immediate DSN and two-connection pool.
func TestStores_TxJoinsAnAlreadyOpenTransaction(t *testing.T) {
	st, db := openTestStores(t)
	ctx := t.Context()

	outerCtx, outerTx, err := st.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = outerTx.Rollback() }()

	innerCtx, innerTx, err := st.Tx(outerCtx)
	require.NoError(t, err)
	assert.Same(t, outerTx, innerTx, "a nested Tx call must return the same *sql.Tx rather than opening a second one")

	require.NoError(t, st.NodeKV.Set(innerCtx, "flow-1", "node-a", "k", "v", 0))
	assert.Equal(t, 1, db.Conn().Stats().InUse)
}
