package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
)

// FlowSummary is one flow file's listing row: identity plus load status, so a
// broken flow file shows up with its error instead of silently vanishing.
type FlowSummary struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Enabled  bool     `json:"enabled"`
	Valid    bool     `json:"valid"`
	Error    string   `json:"error,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// FlowsService exposes the flow definitions to the frontend graph editor.
type FlowsService struct {
	flows *app.FlowsService
}

func NewFlowsService(flows *app.FlowsService) *FlowsService {
	return &FlowsService{flows: flows}
}

// ListFlows returns one summary per flow file, valid and invalid alike.
func (s *FlowsService) ListFlows(ctx context.Context) ([]FlowSummary, error) {
	statuses := s.flows.Statuses(ctx)
	out := make([]FlowSummary, 0, len(statuses))
	for _, st := range statuses {
		summary := FlowSummary{ID: st.ID, Valid: st.Valid, Warnings: st.Warnings}
		if st.Valid {
			summary.Name = st.Flow.Name
			summary.Enabled = st.Flow.Enabled
		} else if st.Err != nil {
			summary.Error = st.Err.Error()
		}
		out = append(out, summary)
	}
	return out, nil
}

func (s *FlowsService) CreateFlow(ctx context.Context, name string) (FlowSummary, error) {
	return summarize(s.flows.Create(ctx, name))
}

func (s *FlowsService) RenameFlow(ctx context.Context, id, name string) (FlowSummary, error) {
	return summarize(s.flows.Rename(ctx, id, name))
}

func (s *FlowsService) SetFlowEnabled(ctx context.Context, id string, enabled bool) (FlowSummary, error) {
	return summarize(s.flows.SetEnabled(ctx, id, enabled))
}

func (s *FlowsService) DeleteFlow(ctx context.Context, id string) error {
	return s.flows.Delete(ctx, id)
}

func (s *FlowsService) GetFlow(ctx context.Context, id string) (flow.Flow, error) {
	return s.flows.Get(ctx, id)
}

func (s *FlowsService) SaveFlow(ctx context.Context, f flow.Flow) error {
	return s.flows.Save(ctx, f)
}

func (s *FlowsService) GetLayout(ctx context.Context, id string) flow.Layout {
	return s.flows.Layout(ctx, id)
}

func (s *FlowsService) SaveLayout(ctx context.Context, id string, layout flow.Layout) error {
	return s.flows.SaveLayout(ctx, id, layout)
}

func (s *FlowsService) GetSidebar(ctx context.Context, id string) flow.SidebarLayout {
	return s.flows.Sidebar(ctx, id)
}

func (s *FlowsService) SaveSidebar(ctx context.Context, id string, layout flow.SidebarLayout) error {
	return s.flows.SaveSidebar(ctx, id, layout)
}

// summarize projects a freshly written flow onto the listing row the editor
// selects with. A flow that just round-tripped through Save is valid by
// construction.
func summarize(f flow.Flow, err error) (FlowSummary, error) {
	if err != nil {
		return FlowSummary{}, err
	}
	return FlowSummary{ID: f.ID, Name: f.Name, Enabled: f.Enabled, Valid: true}, nil
}
