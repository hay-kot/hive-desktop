package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/report"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

type captureUploader struct {
	gzipped []byte
	meta    report.Meta
}

func (u *captureUploader) Upload(_ context.Context, gzipped []byte, meta report.Meta) error {
	u.gzipped = gzipped
	u.meta = meta
	return nil
}

func testPaths(t *testing.T) settings.Paths {
	t.Helper()
	dir := t.TempDir()
	return settings.Paths{
		SettingsPath:         filepath.Join(dir, "settings.yaml"),
		ActionsPath:          filepath.Join(dir, "actions.yml"),
		FlowsDir:             filepath.Join(dir, "flows"),
		CredentialsIndexPath: filepath.Join(dir, "credentials.json"),
		LogFile:              filepath.Join(dir, "desktop.log"),
	}
}

func TestReportSubmit(t *testing.T) {
	paths := testPaths(t)
	up := &captureUploader{}
	svc := newReportService(paths, settings.NewStore(paths.SettingsPath), report.Build{Version: "9.9.9"}, up, zerolog.Nop())

	if !svc.Available() {
		t.Fatal("service should be available with an uploader")
	}

	res, err := svc.Submit(t.Context(), ReportRequest{Description: "broken", IncludeBasics: true})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !strings.HasPrefix(res.ID, "rpt_") {
		t.Errorf("unexpected report id: %q", res.ID)
	}
	if res.ID != up.meta.ReportID {
		t.Errorf("uploaded id %q != returned id %q", up.meta.ReportID, res.ID)
	}
	if up.meta.Version != "9.9.9" {
		t.Errorf("uploaded version %q", up.meta.Version)
	}
	if len(up.gzipped) < 2 || up.gzipped[0] != 0x1f || up.gzipped[1] != 0x8b {
		t.Error("uploaded body is not gzip")
	}
}

func TestReportSubmitUnavailable(t *testing.T) {
	paths := testPaths(t)
	svc := newReportService(paths, settings.NewStore(paths.SettingsPath), report.Build{}, nil, zerolog.Nop())

	if svc.Available() {
		t.Fatal("service should be unavailable without an uploader")
	}
	_, err := svc.Submit(t.Context(), ReportRequest{})
	if err == nil {
		t.Fatal("expected an error with no uploader")
	}
	if KindOf(err) != KindUnavailable {
		t.Errorf("expected KindUnavailable, got %v", KindOf(err))
	}
}
