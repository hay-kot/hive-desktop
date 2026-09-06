package app

import (
	"context"
	"fmt"
	"sort"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
)

// actionUsage answers "is anything still using this action?" by joining the
// loaded flows with the nonterminal output-command queue. It satisfies
// actions.ActionUsageChecker, which is deliberately narrow so the actions
// catalog depends on neither flow nor the queries.
type actionUsage struct {
	flows *flow.FlowStore
	db    *queries.DB
}

func newActionUsage(flows *flow.FlowStore, db *queries.DB) actionUsage {
	return actionUsage{flows: flows, db: db}
}

func (c actionUsage) Usage(ctx context.Context, id string) (actions.ActionUsage, error) {
	usage := actions.ActionUsage{}
	for _, f := range c.flows.List() {
		for _, n := range f.Nodes {
			if cfg, ok := n.Config.(*flow.ActionConfig); ok && cfg.Action == id {
				usage.FlowIDs = append(usage.FlowIDs, f.ID)
				break
			}
		}
	}

	count, err := c.db.CountNonterminalCommandsForAction(ctx, id)
	if err != nil {
		return usage, fmt.Errorf("counting nonterminal output commands: %w", err)
	}
	usage.ActiveCommands = count

	sort.Strings(usage.FlowIDs)
	return usage, nil
}
