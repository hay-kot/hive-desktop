package wailsui

import (
	"context"
	"fmt"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
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
	out, err := s.store.Update(context.Background(), id, a)
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
	err := s.store.Delete(context.Background(), id)
	if err == nil {
		s.wake()
	}
	return err
}
