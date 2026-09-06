package stores

import (
	"context"
	"fmt"

	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// NodeRunStore owns node_run: per-node execution metrics recorded on every
// commit, read back for the flows canvas.
type NodeRunStore struct {
	q *queries.DB
}

func NewNodeRunStore(q *queries.DB, _ Options) *NodeRunStore {
	return &NodeRunStore{q: q}
}

// List returns up to limit of a flow's most recent node_run rows, newest
// first. The frontend canvas derives each node's latest status and a RECENT
// activity list from this single page rather than querying per-node.
func (s *NodeRunStore) List(ctx context.Context, flowID string, limit int) ([]NodeRunRecord, error) {
	rows, err := s.q.Ctx(ctx).ListNodeRunsByFlow(ctx, queries.ListNodeRunsByFlowParams{
		FlowID: flowID,
		Limit:  int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("listing node runs for flow %q: %w", flowID, err)
	}

	runs := make([]NodeRunRecord, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, NodeRunRecord{
			FlowID:    row.FlowID,
			NodeID:    row.NodeID,
			OK:        row.Ok != 0,
			InCount:   int(row.InCount),
			OutCount:  int(row.OutCount),
			DropCount: int(row.DropCount),
			Err:       row.Err.String,
			DurMs:     row.DurMs,
			EndedAt:   row.EndedAt,
		})
	}
	return runs, nil
}
