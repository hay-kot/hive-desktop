package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/colonyops/hive/pkg/osopen"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/report"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// ReportService writes a redacted diagnostic bundle to disk and hands back the
// GitHub issue form to file it against. It never transmits the bundle: the
// user reviews the file and attaches it themselves.
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
		reportsDir: filepath.Join(paths.DataDir, "reports"),
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
	ID       string
	Path     string
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
	return ReportResult{ID: id, Path: path, IssueURL: report.IssueURL(bundle.Build)}, nil
}

// Reveal opens a saved bundle in the OS file manager. The path is checked
// against the reports directory so this cannot be used to reveal an arbitrary
// file, the same rule SystemService.RevealPath follows.
func (s *ReportService) Reveal(_ context.Context, path string) error {
	dir, err := filepath.Abs(s.reportsDir)
	if err != nil {
		return Wrap(err, KindInternal, "resolving the reports directory")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Wrap(err, KindInvalid, "resolving %s", path)
	}
	if filepath.Dir(abs) != dir {
		return Errorf(KindInvalid, "%s is not a saved report", path)
	}
	return Wrap(osopen.Reveal(abs), KindInternal, "revealing %s", abs)
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
