package main

import (
	"strings"
	"testing"

	"github.com/hay-kot/hive-desktop/internal/app/releasenotes"
)

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
