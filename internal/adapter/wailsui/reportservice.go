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
	Description     string `json:"description"`
	Contact         string `json:"contact"`
	IncludeBasics   bool   `json:"includeBasics"`
	IncludeSettings bool   `json:"includeSettings"`
	IncludeFlows    bool   `json:"includeFlows"`
	IncludeActions  bool   `json:"includeActions"`
}

type ReportPreview struct {
	Available    bool `json:"available"`
	HasSettings  bool `json:"hasSettings"`
	FlowCount    int  `json:"flowCount"`
	HasActions   bool `json:"hasActions"`
	AccountCount int  `json:"accountCount"`
	HasLogs      bool `json:"hasLogs"`
	LogBytes     int  `json:"logBytes"`
}

type ReportResult struct {
	ID string `json:"id"`
}

func (s *ReportService) Preview(ctx context.Context) ReportPreview {
	p := s.report.Preview(ctx)
	return ReportPreview{
		Available:    p.Available,
		HasSettings:  p.HasSettings,
		FlowCount:    p.FlowCount,
		HasActions:   p.HasActions,
		AccountCount: p.AccountCount,
		HasLogs:      p.HasLogs,
		LogBytes:     p.LogBytes,
	}
}

func (s *ReportService) Submit(ctx context.Context, in ReportInput) (ReportResult, error) {
	res, err := s.report.Submit(ctx, app.ReportRequest{
		Description:     in.Description,
		Contact:         in.Contact,
		IncludeBasics:   in.IncludeBasics,
		IncludeSettings: in.IncludeSettings,
		IncludeFlows:    in.IncludeFlows,
		IncludeActions:  in.IncludeActions,
	})
	if err != nil {
		return ReportResult{}, err
	}
	return ReportResult{ID: res.ID}, nil
}
