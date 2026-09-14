package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app"
)

type ReportService struct {
	report *app.ReportService
}

func NewReportService(report *app.ReportService) *ReportService {
	return &ReportService{report: report}
}

type ReportInput struct {
	IncludeLogs     bool `json:"includeLogs"`
	IncludeSettings bool `json:"includeSettings"`
	IncludeFlows    bool `json:"includeFlows"`
	IncludeActions  bool `json:"includeActions"`
}

type ReportPreview struct {
	HasSettings bool `json:"hasSettings"`
	FlowCount   int  `json:"flowCount"`
	HasActions  bool `json:"hasActions"`
	HasLogs     bool `json:"hasLogs"`
	LogBytes    int  `json:"logBytes"`
}

type ReportResult struct {
	Path     string `json:"path"`
	Dir      string `json:"dir"`
	IssueURL string `json:"issueUrl"`
}

func (s *ReportService) Preview(ctx context.Context) ReportPreview {
	p := s.report.Preview(ctx)
	return ReportPreview{
		HasSettings: p.HasSettings,
		FlowCount:   p.FlowCount,
		HasActions:  p.HasActions,
		HasLogs:     p.HasLogs,
		LogBytes:    p.LogBytes,
	}
}

// Save writes the bundle to disk and returns where it landed plus the issue
// form to file it against. The frontend opens that URL; nothing is uploaded.
func (s *ReportService) Save(ctx context.Context, in ReportInput) (ReportResult, error) {
	res, err := s.report.Save(ctx, app.ReportRequest{
		IncludeLogs:     in.IncludeLogs,
		IncludeSettings: in.IncludeSettings,
		IncludeFlows:    in.IncludeFlows,
		IncludeActions:  in.IncludeActions,
	})
	if err != nil {
		return ReportResult{}, err
	}
	return ReportResult{Path: res.Path, Dir: res.Dir, IssueURL: res.IssueURL}, nil
}
