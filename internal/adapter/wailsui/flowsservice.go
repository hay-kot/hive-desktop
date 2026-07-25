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
func (s *FlowsService) ListFlows() ([]FlowSummary, error) {
	statuses := s.flows.Statuses(context.Background())
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

func (s *FlowsService) CreateFlow(name string) (FlowSummary, error) {
	return summarize(s.flows.Create(context.Background(), name))
}

func (s *FlowsService) RenameFlow(id, name string) (FlowSummary, error) {
	return summarize(s.flows.Rename(context.Background(), id, name))
}

func (s *FlowsService) SetFlowEnabled(id string, enabled bool) (FlowSummary, error) {
	return summarize(s.flows.SetEnabled(context.Background(), id, enabled))
}

func (s *FlowsService) DeleteFlow(id string) error {
	return s.flows.Delete(context.Background(), id)
}

func (s *FlowsService) GetFlow(id string) (flow.Flow, error) {
	return s.flows.Get(context.Background(), id)
}

func (s *FlowsService) SaveFlow(f flow.Flow) error {
	return s.flows.Save(context.Background(), f)
}

func (s *FlowsService) GetLayout(id string) flow.Layout {
	return s.flows.Layout(context.Background(), id)
}

func (s *FlowsService) SaveLayout(id string, layout flow.Layout) error {
	return s.flows.SaveLayout(context.Background(), id, layout)
}

func (s *FlowsService) GetSidebar(id string) flow.SidebarLayout {
	return s.flows.Sidebar(context.Background(), id)
}

func (s *FlowsService) SaveSidebar(id string, layout flow.SidebarLayout) error {
	return s.flows.SaveSidebar(context.Background(), id, layout)
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
