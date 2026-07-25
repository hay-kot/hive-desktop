package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/actions"
)

// ActionsService is the explicit editor API for the global actions catalog.
type ActionsService struct {
	actions *app.ActionsService
}

func NewActionsService(catalog *app.ActionsService) *ActionsService {
	return &ActionsService{actions: catalog}
}

func (s *ActionsService) ListActions() actions.EditableCatalog {
	return s.actions.List(context.Background())
}

func (s *ActionsService) GetAction(id string) (actions.EditableAction, error) {
	return s.actions.Get(context.Background(), id)
}

func (s *ActionsService) CreateAction(a actions.EditableAction) (actions.EditableAction, error) {
	return s.actions.Create(context.Background(), a)
}

func (s *ActionsService) UpdateAction(id string, a actions.EditableAction) (actions.EditableAction, error) {
	return s.actions.Update(context.Background(), id, a)
}

// ReorderActions persists the catalog order the settings list was dragged
// into. ids must be the full catalog.
func (s *ActionsService) ReorderActions(ids []string) error {
	return s.actions.Reorder(context.Background(), ids)
}

func (s *ActionsService) DeleteAction(id string) error {
	return s.actions.Delete(context.Background(), id)
}
