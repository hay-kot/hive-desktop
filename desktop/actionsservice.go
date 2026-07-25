package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/desktop/pipeline/actions"
)

// ActionsService is the explicit editor API for the global actions catalog.
type ActionsService struct {
	store *actions.ActionStore
	wake  func()
}

// NewActionsService accepts the wake callback explicitly so successful-write
// notification is testable without Wails global state.
func NewActionsService(catalog *actions.ActionStore, wake func()) *ActionsService {
	if wake == nil {
		wake = func() {}
	}
	return &ActionsService{store: catalog, wake: wake}
}

// ListActions returns the effective last-good catalog and a current parse
// error, if a hand edit made the latest actions.yml invalid.
func (s *ActionsService) ListActions() actions.EditableCatalog { return s.store.ListEditable() }

func (s *ActionsService) GetAction(id string) (actions.EditableAction, error) {
	a, ok := s.store.GetEditable(id)
	if !ok {
		return actions.EditableAction{}, fmt.Errorf("action %q not found", id)
	}
	return a, nil
}

func (s *ActionsService) CreateAction(a actions.EditableAction) (actions.EditableAction, error) {
	out, err := s.store.Create(a)
	if err == nil {
		s.wake()
	}
	return out, err
}

func (s *ActionsService) UpdateAction(id string, a actions.EditableAction) (actions.EditableAction, error) {
	out, err := s.store.Update(id, a)
	if err == nil {
		s.wake()
	}
	return out, err
}

// ReorderActions persists the catalog order the settings list was dragged
// into. ids must be the full catalog; a stale list (a hand edit added or
// removed an action meanwhile) is rejected so the caller reloads.
func (s *ActionsService) ReorderActions(ids []string) error {
	err := s.store.Reorder(ids)
	if err == nil {
		s.wake()
	}
	return err
}

func (s *ActionsService) DeleteAction(id string) error {
	err := s.store.Delete(id)
	if err == nil {
		s.wake()
	}
	return err
}

// actionUsageChecker joins the loaded flows and nonterminal output commands.
type actionUsageChecker struct {
	flows *flow.FlowStore
	db    *store.DB
}

func (c actionUsageChecker) Usage(id string) (actions.ActionUsage, error) {
	usage := actions.ActionUsage{}
	for _, f := range c.flows.List() {
		for _, n := range f.Nodes {
			if cfg, ok := n.Config.(*flow.ActionConfig); ok && cfg.Action == id {
				usage.FlowIDs = append(usage.FlowIDs, f.ID)
				break
			}
		}
	}
	// Pending work can run and running work may have been dispatched. Done and
	// failed history is terminal and must never keep an action from deletion.
	err := c.db.Conn().QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM output_command
		WHERE action_id = ? AND status IN ('pending', 'running')`, id).Scan(&usage.ActiveCommands)
	if err != nil {
		return usage, fmt.Errorf("counting nonterminal output commands: %w", err)
	}
	sort.Strings(usage.FlowIDs)
	return usage, nil
}
