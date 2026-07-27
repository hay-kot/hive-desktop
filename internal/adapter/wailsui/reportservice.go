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
	Description string `json:"description"`
	Contact     string `json:"contact"`
	IncludeLogs bool   `json:"includeLogs"`
}

type ReportPreview struct {
	Available   bool     `json:"available"`
	Attachments []string `json:"attachments"`
	LogIncluded bool     `json:"logIncluded"`
	LogPath     string   `json:"logPath"`
	LogBytes    int      `json:"logBytes"`
	LogContent  string   `json:"logContent"`
}

type ReportResult struct {
	ID string `json:"id"`
}

func (s *ReportService) Preview(ctx context.Context, in ReportInput) ReportPreview {
	p := s.report.Preview(ctx, app.ReportRequest{
		Description: in.Description,
		Contact:     in.Contact,
		IncludeLogs: in.IncludeLogs,
	})
	return ReportPreview{
		Available:   p.Available,
		Attachments: p.Attachments,
		LogIncluded: p.LogIncluded,
		LogPath:     p.LogPath,
		LogBytes:    p.LogBytes,
		LogContent:  p.LogContent,
	}
}

func (s *ReportService) Submit(ctx context.Context, in ReportInput) (ReportResult, error) {
	res, err := s.report.Submit(ctx, app.ReportRequest{
		Description: in.Description,
		Contact:     in.Contact,
		IncludeLogs: in.IncludeLogs,
	})
	if err != nil {
		return ReportResult{}, err
	}
	return ReportResult{ID: res.ID}, nil
}
