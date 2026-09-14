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
	Path string `json:"path"`
	Dir  string `json:"dir"`
}

// IssueURL is what "Report a problem" opens. No bundle is built.
func (s *ReportService) IssueURL(ctx context.Context) string {
	return s.report.IssueURL(ctx)
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

// Save writes the bundle and returns where it went. It does not touch the
// issue flow: the bundle is private and the issue is not.
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
	return ReportResult{Path: res.Path, Dir: res.Dir}, nil
}
