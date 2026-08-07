package main

import (
	"strings"
	"testing"

	"github.com/hay-kot/hive-desktop/internal/app/releasenotes"
)

// previousReleaseVersion picks the baseline a scaffolded entry's commit range
// starts from. The cases that matter are the promotion path — semver orders
// the words "beta" and "dev" the opposite way round from this product.
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

func TestReleaseNotesBody(t *testing.T) {
	body := releaseNotesBody(
		mustVersion(t, "1.4.0-dev.2"),
		releasenotes.Entry{Version: "1.4.0-dev.2", Summary: "A short line.", Body: "## Added\n\n- a thing"},
		"https://dl.hivedesktop.com",
	)

	for _, want := range []string{
		"https://dl.hivedesktop.com/desktop/releases/1.4.0-dev.2/SHA256SUMS",
		"A short line.",
		"## Added\n\n- a thing",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

// An entry whose summary says nothing must not leave a stray blank stanza
// between the header and the notes.
func TestReleaseNotesBodyOmitsAnAbsentSummary(t *testing.T) {
	version := mustVersion(t, "1.4.0")
	entry := releasenotes.Entry{Version: "1.4.0", Body: "## Fixed\n\n- a thing"}

	body := releaseNotesBody(version, entry, "https://dl.hivedesktop.com")
	want := releaseNotesHeader(version, "https://dl.hivedesktop.com") + "\n## Fixed\n\n- a thing\n"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}
