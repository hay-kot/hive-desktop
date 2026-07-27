package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/report"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// ReportService assembles a redacted diagnostic bundle and sends it to the
// ingest endpoint. It is unavailable when no uploader is wired (a build with
// no report token), and Submit fails cleanly in that case.
type ReportService struct {
	assembler *report.Assembler
	settings  *settings.Store
	uploader  report.Uploader
	logger    zerolog.Logger
}

func newReportService(paths settings.Paths, store *settings.Store, build report.Build, uploader report.Uploader, logger zerolog.Logger) *ReportService {
	return &ReportService{
		assembler: report.NewAssembler(paths, build),
		settings:  store,
		uploader:  uploader,
		logger:    logger,
	}
}

type ReportRequest struct {
	Description string
	Contact     string
	IncludeLogs bool
}

type ReportResult struct {
	ID string
}

// ReportPreview is what a report would contain, so the user can review the
// attachment and opt out of the logs before sending.
type ReportPreview struct {
	Available   bool
	Attachments []string
	LogIncluded bool
	LogPath     string
	LogBytes    int
	LogContent  string
}

func (s *ReportService) Available() bool { return s.uploader != nil }

func (s *ReportService) Preview(_ context.Context, req ReportRequest) ReportPreview {
	bundle := s.assemble("", req)
	p := ReportPreview{
		Available:   s.uploader != nil,
		Attachments: attachmentSummary(bundle),
		LogIncluded: bundle.Logs != nil,
	}
	if bundle.Logs != nil {
		p.LogPath = bundle.Logs.Path
		p.LogBytes = bundle.Logs.Bytes
		p.LogContent = bundle.Logs.Content
	}
	return p
}

func (s *ReportService) Submit(ctx context.Context, req ReportRequest) (ReportResult, error) {
	if s.uploader == nil {
		return ReportResult{}, Errorf(KindUnavailable, "problem reporting is not available in this build")
	}

	id := newReportID()
	bundle := s.assemble(id, req)

	gz, err := report.GzipJSON(bundle)
	if err != nil {
		return ReportResult{}, Wrap(err, KindInternal, "compressing the diagnostic bundle")
	}
	if len(gz) > report.MaxUploadBytes {
		return ReportResult{}, Errorf(KindInvalid, "diagnostic bundle is too large to send")
	}

	meta := report.Meta{ReportID: id, Version: bundle.Build.Version, OS: bundle.Build.OS, Arch: bundle.Build.Arch}
	if err := s.uploader.Upload(ctx, gz, meta); err != nil {
		return ReportResult{}, Wrap(err, KindUnavailable, "sending the report")
	}

	s.logger.Info().Str("report_id", id).Int("bytes", len(gz)).Msg("diagnostic report submitted")
	return ReportResult{ID: id}, nil
}

func (s *ReportService) assemble(id string, req ReportRequest) *report.Bundle {
	return s.assembler.Assemble(id, time.Now().UTC(), report.Options{
		Description: req.Description,
		Contact:     req.Contact,
		IncludeLogs: req.IncludeLogs,
		Channel:     s.channel(),
	})
}

func (s *ReportService) channel() string {
	cfg, err := s.settings.Effective()
	if err != nil {
		return ""
	}
	return cfg.Updates.Channel
}

func newReportID() string {
	return "rpt_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func attachmentSummary(b *report.Bundle) []string {
	items := []string{"Build and system info"}
	if b.Logs != nil {
		items = append(items, fmt.Sprintf("Recent logs (%d KB)", (b.Logs.Bytes+1023)/1024))
	}
	if b.Config.Settings != nil {
		items = append(items, "Redacted settings")
	}
	if n := len(b.Config.Flows); n > 0 {
		items = append(items, fmt.Sprintf("Redacted flows (%d)", n))
	}
	if b.Config.Actions != nil {
		items = append(items, "Redacted actions")
	}
	if n := len(b.Config.Accounts); n > 0 {
		items = append(items, fmt.Sprintf("Connected accounts (%d)", n))
	}
	return items
}
