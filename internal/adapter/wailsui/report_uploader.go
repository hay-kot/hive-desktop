package wailsui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/report"
)

// DefaultReportEndpoint is the ingest route on the landing worker. Decision
// 0003's philosophy applies: the app bakes in a stable domain, never a bucket.
const DefaultReportEndpoint = "https://hivedesktop.com/api/report"

// reportToken is the shared client token, stamped at release via
// -X ...wailsui.reportToken=VALUE. Empty in source builds, which disables
// problem reporting entirely (ReportUploader returns nil).
var reportToken = ""

type reportUploader struct {
	endpoint string
	token    string
	client   *http.Client
}

func newReportUploader(endpoint, token string) *reportUploader {
	return &reportUploader{
		endpoint: endpoint,
		token:    token,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (u *reportUploader) Upload(ctx context.Context, gzipped []byte, meta report.Meta) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.endpoint, bytes.NewReader(gzipped))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Authorization", "Bearer "+u.token)
	req.Header.Set("X-Hive-Report-Id", meta.ReportID)
	req.Header.Set("X-Hive-Version", meta.Version)
	req.Header.Set("X-Hive-Os", meta.OS)
	req.Header.Set("X-Hive-Arch", meta.Arch)

	resp, err := u.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("report endpoint returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}

// ReportUploader is the driven port app.Config.ReportUploader takes. It is nil
// when no report token is stamped into the build, disabling reporting.
func (u *UI) ReportUploader() report.Uploader {
	if reportToken == "" {
		return nil
	}
	return newReportUploader(DefaultReportEndpoint, reportToken)
}
