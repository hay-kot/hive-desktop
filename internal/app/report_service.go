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

// ReportService owns the two halves of a bug report, which are deliberately
// not one action. IssueURL is the public half: a bug form with nothing in it
// but the build identity. Save is the private half: a redacted bundle written
// to disk, which the user sends to a maintainer only when one asks for it.
//
// Nothing here transmits the bundle, and the two must not be recombined. A
// bundle names the user's paths, hosts and repositories, and an issue is
// world-readable (ADR problem-reports-are-github-issues).
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
	Path string
	// Dir is returned rather than derived from Path because the caller is the
	// webview, which has no path handling of its own.
	Dir string
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

// IssueURL is the whole of "Report a problem": it builds no bundle and reads
// no config, so the action cannot leak anything the About pane does not show.
func (s *ReportService) IssueURL(_ context.Context) string {
	return report.IssueURL(s.assembler.BuildInfo(s.channel()))
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
	return ReportResult{Path: path, Dir: s.reportsDir}, nil
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
