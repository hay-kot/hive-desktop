package app

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/report"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

func testPaths(t *testing.T) settings.Paths {
	t.Helper()
	dir := t.TempDir()
	return settings.Paths{
		DataDir:      dir,
		SettingsPath: filepath.Join(dir, "settings.yaml"),
		ActionsPath:  filepath.Join(dir, "actions.yml"),
		FlowsDir:     filepath.Join(dir, "flows"),
		LogFile:      filepath.Join(dir, "desktop.log"),
	}
}

func TestReportSave(t *testing.T) {
	paths := testPaths(t)
	svc := newReportService(paths, settings.NewStore(paths.SettingsPath), report.Build{Version: "9.9.9"}, zerolog.Nop())

	res, err := svc.Save(t.Context(), ReportRequest{})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.HasPrefix(res.ID, "rpt_") {
		t.Errorf("unexpected report id: %q", res.ID)
	}
	if got, want := filepath.Dir(res.Path), filepath.Join(paths.DataDir, "reports"); got != want {
		t.Errorf("saved to %q, want a file in %q", got, want)
	}
	if !strings.Contains(res.IssueURL, "template=bug.yml") {
		t.Errorf("issue url does not target the bug form: %q", res.IssueURL)
	}

	gz, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if len(gz) < 2 || gz[0] != 0x1f || gz[1] != 0x8b {
		t.Fatal("saved bundle is not gzip")
	}

	zr, err := gzip.NewReader(strings.NewReader(string(gz)))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	var bundle report.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if bundle.Build.Version != "9.9.9" {
		t.Errorf("build info missing from the bundle: %+v", bundle.Build)
	}
}

// Nothing beyond build info goes in unless it is asked for: the bundle ends up
// on a public issue.
func TestReportSaveOmitsEverySurfaceByDefault(t *testing.T) {
	paths := testPaths(t)
	if err := os.WriteFile(paths.LogFile, []byte("2026-09-14 INF /Users/somebody/private-repo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.SettingsPath, []byte("updates:\n  channel: beta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := newReportService(paths, settings.NewStore(paths.SettingsPath), report.Build{Version: "1.0.0"}, zerolog.Nop())

	res, err := svc.Save(t.Context(), ReportRequest{})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if strings.Contains(readBundle(t, res.Path), "private-repo") {
		t.Error("log content reached a bundle that did not ask for it")
	}
}

func TestReportRevealRejectsPathsOutsideReports(t *testing.T) {
	paths := testPaths(t)
	svc := newReportService(paths, settings.NewStore(paths.SettingsPath), report.Build{}, zerolog.Nop())

	err := svc.Reveal(t.Context(), filepath.Join(paths.DataDir, "settings.yaml"))
	if err == nil {
		t.Fatal("expected a rejection outside the reports directory")
	}
	if KindOf(err) != KindInvalid {
		t.Errorf("expected KindInvalid, got %v", KindOf(err))
	}
}

func readBundle(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
