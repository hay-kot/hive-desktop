package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/report"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// ReportService writes a redacted diagnostic bundle to disk and hands back the
// GitHub issue form to file it against. It never transmits the bundle: the
// user reviews the file and attaches it themselves.
//
// The saved file is shown through SystemService.OpenPath, which already guards
// the app's known locations; ReportsDir is one of them.
type ReportService struct {
	assembler  *report.Assembler
	settings   *settings.Store
	reportsDir string
	logger     zerolog.Logger
}

func newReportService(paths settings.Paths, store *settings.Store, build report.Build, logger zerolog.Logger) *ReportService {
	return &ReportService{
		assembler:  report.NewAssembler(paths, build),
		settings:   store,
		reportsDir: paths.ReportsDir,
		logger:     logger,
	}
}

type ReportRequest struct {
	IncludeLogs     bool
	IncludeSettings bool
	IncludeFlows    bool
	IncludeActions  bool
}

type ReportResult struct {
	// Path is the saved bundle; Dir is the directory to show it in.
	Path     string
	Dir      string
	IssueURL string
}

// ReportPreview is the inventory of what a report could attach, so the dialog
// can label each toggle and hide the ones with nothing behind them.
type ReportPreview struct {
	HasSettings bool
	FlowCount   int
	HasActions  bool
	HasLogs     bool
	LogBytes    int
}

func (s *ReportService) Preview(_ context.Context) ReportPreview {
	inv := s.assembler.Inventory()
	return ReportPreview{
		HasSettings: inv.HasSettings,
		FlowCount:   inv.FlowCount,
		HasActions:  inv.HasActions,
		HasLogs:     inv.HasLogs,
		LogBytes:    inv.LogBytes,
	}
}

// Save writes the bundle under the data directory and returns its path with
// the issue URL to file it against.
func (s *ReportService) Save(_ context.Context, req ReportRequest) (ReportResult, error) {
	id := newReportID()
	bundle := s.assembler.Assemble(id, time.Now().UTC(), report.Options{
		Channel:         s.channel(),
		IncludeLogs:     req.IncludeLogs,
		IncludeSettings: req.IncludeSettings,
		IncludeFlows:    req.IncludeFlows,
		IncludeActions:  req.IncludeActions,
	})

	gz, err := report.GzipJSON(bundle)
	if err != nil {
		return ReportResult{}, Wrap(err, KindInternal, "compressing the diagnostic bundle")
	}
	if len(gz) > report.MaxBundleBytes {
		return ReportResult{}, Errorf(KindInvalid, "diagnostic bundle is too large to attach to an issue")
	}

	if err := os.MkdirAll(s.reportsDir, 0o755); err != nil {
		return ReportResult{}, Wrap(err, KindInternal, "creating the reports directory")
	}
	path := filepath.Join(s.reportsDir, "hive-report-"+id+".json.gz")
	if err := os.WriteFile(path, gz, 0o600); err != nil {
		return ReportResult{}, Wrap(err, KindInternal, "writing %s", path)
	}

	s.logger.Info().Str("report_id", id).Str("path", path).Int("bytes", len(gz)).Msg("diagnostic bundle saved")
	return ReportResult{Path: path, Dir: s.reportsDir, IssueURL: report.IssueURL(bundle.Build)}, nil
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
