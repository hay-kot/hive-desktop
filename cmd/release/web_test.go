package main

import "testing"

func TestSiteBaseURL(t *testing.T) {
	if got := siteBaseURL(); got != defaultSiteBase {
		t.Errorf("siteBaseURL() = %q, want default %q", got, defaultSiteBase)
	}
	t.Setenv("HIVE_DESKTOP_SITE_BASE", "https://staging.example.com/")
	if got := siteBaseURL(); got != "https://staging.example.com" {
		t.Errorf("siteBaseURL() = %q, want trimmed override", got)
	}
}
