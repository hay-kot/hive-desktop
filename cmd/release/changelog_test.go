package main

import (
	"strings"
	"testing"

	"github.com/hay-kot/hive-desktop/internal/app/releasenotes"
)

// A prerelease publishes whatever the draft says and is never gated on an
// entry of its own — that is what makes cutting one cost no changelog work.
func TestNotesForAPrereleaseUsesTheDraft(t *testing.T) {
	entries, err := releasenotes.Load()
	if err != nil {
		t.Fatalf("load changelog: %v", err)
	}
	draft, ok := entries.Draft()
	if !ok {
		t.Skip("no draft committed; nothing to compare against")
	}

	entry, err := notesFor(mustVersion(t, "99.0.0-dev.1"))
	if err != nil {
		t.Fatalf("notesFor: %v", err)
	}
	if entry.Body != draft.Body || entry.Summary != draft.Summary {
		t.Fatalf("prerelease notes = %+v, want the draft %+v", entry, draft)
	}
}

// The gate that stands between an unreleased draft and a stable release: the
// draft has to be promoted, under the version's own name, before publishing.
func TestNotesForAStableReleaseRequiresAPromotedEntry(t *testing.T) {
	_, err := notesFor(mustVersion(t, "99.0.0"))
	if err == nil {
		t.Fatal("expected a stable release with no entry to be rejected")
	}
	if !strings.Contains(err.Error(), "changelog:promote") {
		t.Fatalf("error should name the promote command, got %q", err)
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
