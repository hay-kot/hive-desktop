package main

import (
	"net/http"
	"testing"
)

func TestReportProbeResult(t *testing.T) {
	tests := []struct {
		status  int
		wantErr bool
	}{
		{http.StatusUnsupportedMediaType, false},
		{http.StatusUnauthorized, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusOK, true},
		{http.StatusNotFound, true},
	}
	for _, tt := range tests {
		if err := reportProbeResult(tt.status); (err != nil) != tt.wantErr {
			t.Errorf("reportProbeResult(%d) err = %v, wantErr %v", tt.status, err, tt.wantErr)
		}
	}
}

func TestSiteBaseURL(t *testing.T) {
	if got := siteBaseURL(); got != defaultSiteBase {
		t.Errorf("siteBaseURL() = %q, want default %q", got, defaultSiteBase)
	}
	t.Setenv("HIVE_DESKTOP_SITE_BASE", "https://staging.example.com/")
	if got := siteBaseURL(); got != "https://staging.example.com" {
		t.Errorf("siteBaseURL() = %q, want trimmed override", got)
	}
}
