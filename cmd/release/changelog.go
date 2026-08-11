package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/releasenotes"
)

// changelogDir is where an entry has to live to be embedded. The changelog
// sits inside the package that serves it because go:embed cannot reach above
// its own directory, the same arrangement flow/docs and actions/docs use.
const changelogDir = "internal/app/releasenotes/changelog"

func draftPath() string { return filepath.Join(changelogDir, releasenotes.DraftFile) }

func entryPath(version releaseVersion) string {
	return filepath.Join(changelogDir, version.String()+".md")
}

// notesFor returns the release notes to publish with version.
//
// A stable release has an entry of its own, promoted from the draft before the
// release commit. A prerelease has none and carries the draft instead, which
// is exactly what its binary embeds — so the GitHub release body and the
// channel manifest say what the app itself will say.
//
// It reads through internal/app/releasenotes — the same embedded changelog the
// app serves — rather than off disk, so a published version and the notes
// describing it cannot drift apart.
func notesFor(version releaseVersion) (releasenotes.Entry, error) {
	entries, err := releasenotes.Load()
	if err != nil {
		return releasenotes.Entry{}, fmt.Errorf("read changelog: %w", err)
	}
	if entry, ok := entries.Find(version.String()); ok {
		return entry, nil
	}
	if version.channel() == "stable" {
		return releasenotes.Entry{}, fmt.Errorf(
			"no changelog entry for %s: promote the draft with `mise run changelog:promote -- %s` and commit it before releasing",
			version, version)
	}
	entry, _ := entries.Draft()
	return entry, nil
}

// validateChangelogEntry is the release gate. It runs with the other fail-fast
// checks, before anything is built: notes are embedded in the binary, so an
// entry written after the build would describe a release that cannot show it.
//
// Only a stable release is gated. A prerelease publishes whatever the draft
// says, including nothing — which is the point, since cutting one is meant to
// cost no changelog work at all.
func validateChangelogEntry(version releaseVersion) error {
	_, err := notesFor(version)
	return err
}

// releaseNotesBody assembles the GitHub release body: the R2 download header
// followed by the release notes. downloadBase is a parameter rather than a
// call to downloadBaseURL so the assembled body can be asserted against a
// literal, the same seam releaseNotesHeader has.
func releaseNotesBody(version releaseVersion, entry releasenotes.Entry, downloadBase string) string {
	body := releaseNotesHeader(version, downloadBase)
	if entry.Summary != "" {
		body += "\n" + entry.Summary + "\n"
	}
	return body + "\n" + entry.Body + "\n"
}

// promoteTargetVersion resolves the promote command's argument. "stable" picks
// the next stable version from the same sources planRelease uses — tags *and*
// the live manifests — because the R2 history predates this repository, and a
// promotion named off tags alone would write an entry the release then refuses
// to find.
func promoteTargetVersion(ctx context.Context, arg string) (releaseVersion, error) {
	if arg != "stable" {
		version, err := parsePublishVersion(arg)
		if err != nil {
			return releaseVersion{}, fmt.Errorf("invalid version %q: %w", arg, err)
		}
		if version.channel() != "stable" {
			return releaseVersion{}, fmt.Errorf(
				"%s is a prerelease: only a stable release gets an entry of its own, and a prerelease publishes the draft as it stands", version)
		}
		return version, nil
	}
	versions, _, err := releaseVersions(ctx)
	if err != nil {
		return releaseVersion{}, err
	}
	return parsePublishVersion(nextVersion("stable", versions))
}

// promoteDraft turns the accumulated draft into version's entry and leaves an
// empty draft behind for the next cycle. The bytes are moved unchanged: what
// prereleases have been showing all along is what the stable release ships.
func promoteDraft(version releaseVersion) (string, error) {
	entries, err := releasenotes.Load()
	if err != nil {
		return "", fmt.Errorf("read changelog: %w", err)
	}
	draft, ok := entries.Draft()
	if !ok {
		return "", fmt.Errorf("%s is empty: there is nothing to release", draftPath())
	}

	path := entryPath(version)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	}

	header := fmt.Sprintf("---\nversion: %s\ndate: %s\nsummary: %q\n---\n\n",
		version, time.Now().Format(time.DateOnly), draft.Summary)
	if err := os.WriteFile(path, []byte(header+draft.Body+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("write changelog entry: %w", err)
	}
	if err := os.WriteFile(draftPath(), []byte(emptyDraft), 0o644); err != nil {
		return "", fmt.Errorf("reset %s: %w", draftPath(), err)
	}
	return path, nil
}

// emptyDraft is what next.md holds between a promotion and the next change to
// land. The file has to exist even when it says nothing: go:embed fails to
// compile on a pattern that matches no files.
const emptyDraft = `---
summary: ""
---
`
