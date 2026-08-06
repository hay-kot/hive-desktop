package main

import (
	"strings"
	"testing"
)

func mustVersion(t *testing.T, value string) releaseVersion {
	t.Helper()
	version, err := parsePublishVersion(value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return version
}

func TestReleaseNotesHeader(t *testing.T) {
	header := releaseNotesHeader(mustVersion(t, "1.4.0-dev.2"), "https://dl.hivedesktop.com")
	for _, want := range []string{
		"dev channel",
		"https://dl.hivedesktop.com/desktop/releases/1.4.0-dev.2/",
		"https://dl.hivedesktop.com/desktop/releases/1.4.0-dev.2/SHA256SUMS",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("header missing %q:\n%s", want, header)
		}
	}
}

func TestReleaseTitle(t *testing.T) {
	if got := releaseTitle(mustVersion(t, "1.4.0-beta.1")); got != "Hive Desktop 1.4.0-beta.1" {
		t.Fatalf("title = %q", got)
	}
}
