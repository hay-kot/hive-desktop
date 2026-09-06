package app

import (
	"context"
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
	Description     string
	Contact         string
	IncludeBasics   bool
	IncludeSettings bool
	IncludeFlows    bool
	IncludeActions  bool
}

type ReportResult struct {
	ID string
}

// ReportPreview is the inventory of what a report could attach, so the dialog
// can label each toggle and hide the ones with nothing behind them.
type ReportPreview struct {
	Available    bool
	HasSettings  bool
	FlowCount    int
	HasActions   bool
	AccountCount int
	HasLogs      bool
	LogBytes     int
}

func (s *ReportService) Available() bool { return s.uploader != nil }

func (s *ReportService) Preview(_ context.Context) ReportPreview {
	inv := s.assembler.Inventory()
	return ReportPreview{
		Available:    s.uploader != nil,
		HasSettings:  inv.HasSettings,
		FlowCount:    inv.FlowCount,
		HasActions:   inv.HasActions,
		AccountCount: inv.AccountCount,
		HasLogs:      inv.HasLogs,
		LogBytes:     inv.LogBytes,
	}
}

func (s *ReportService) Submit(ctx context.Context, req ReportRequest) (ReportResult, error) {
	if s.uploader == nil {
		// unavailable: this build carries no report token, so no uploader is wired.
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

	meta := report.Meta{ReportID: id}
	if bundle.Build != nil {
		meta.Version, meta.OS, meta.Arch = bundle.Build.Version, bundle.Build.OS, bundle.Build.Arch
	}
	if err := s.uploader.Upload(ctx, gz, meta); err != nil {
		// unavailable: the upload itself failed (offline, endpoint down, refused).
		return ReportResult{}, Wrap(err, KindUnavailable, "sending the report")
	}

	s.logger.Info().Str("report_id", id).Int("bytes", len(gz)).Msg("diagnostic report submitted")
	return ReportResult{ID: id}, nil
}

func (s *ReportService) assemble(id string, req ReportRequest) *report.Bundle {
	return s.assembler.Assemble(id, time.Now().UTC(), report.Options{
		Description:     req.Description,
		Contact:         req.Contact,
		Channel:         s.channel(),
		IncludeBasics:   req.IncludeBasics,
		IncludeSettings: req.IncludeSettings,
		IncludeFlows:    req.IncludeFlows,
		IncludeActions:  req.IncludeActions,
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
