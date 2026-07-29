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

func TestPreviousReleaseVersion(t *testing.T) {
	tags := []releaseVersion{
		mustVersion(t, "0.1.1-dev.1"),
		mustVersion(t, "0.1.8-dev.1"),
		mustVersion(t, "0.1.8-dev.2"),
		mustVersion(t, "0.1.8-beta.1"),
		mustVersion(t, "0.1.8"),
	}

	tests := []struct {
		name    string
		version string
		want    string
		found   bool
	}{
		{name: "next dev after beta", version: "0.1.9-dev.1", want: "0.1.8", found: true},
		{name: "beta promotes latest dev", version: "0.1.8-beta.1", want: "0.1.8-dev.2", found: true},
		{name: "stable promotes beta", version: "0.1.8", want: "0.1.8-beta.1", found: true},
		{name: "same-channel increment", version: "0.1.8-dev.3", want: "0.1.8-dev.2", found: true},
		{name: "earliest has no predecessor", version: "0.1.1-dev.1", found: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous, found := previousReleaseVersion(mustVersion(t, tt.version), tags)
			if found != tt.found {
				t.Fatalf("found = %t, want %t", found, tt.found)
			}
			if found && previous.String() != tt.want {
				t.Fatalf("previous = %s, want %s", previous.String(), tt.want)
			}
		})
	}
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
