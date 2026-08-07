package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hay-kot/hive-desktop/internal/app/releasenotes"
)

// changelogDir is where an entry has to live to be embedded. The changelog
// sits inside the package that serves it because go:embed cannot reach above
// its own directory, the same arrangement flow/docs and actions/docs use.
const changelogDir = "internal/app/releasenotes/changelog"

// changelogEntry returns the release notes for version.
//
// It reads them through internal/app/releasenotes — the same embedded
// changelog the app itself serves — rather than off disk, so the gate below
// validates the exact bytes the shipped binary carries. A published version
// and the notes describing it cannot drift apart.
func changelogEntry(version releaseVersion) (releasenotes.Entry, error) {
	entries, err := releasenotes.Load()
	if err != nil {
		return releasenotes.Entry{}, fmt.Errorf("read changelog: %w", err)
	}
	entry, ok := entries.Find(version.String())
	if !ok {
		return releasenotes.Entry{}, fmt.Errorf(
			"no changelog entry for %s: write %s/%s.md and commit it before releasing "+
				"(mise run changelog:new -- %s scaffolds one)",
			version, changelogDir, version, version)
	}
	return entry, nil
}

// validateChangelogEntry is the release gate. It runs with the other fail-fast
// checks, before anything is built: notes are embedded in the binary, so an
// entry written after the build would describe a release that cannot show it.
func validateChangelogEntry(version releaseVersion) error {
	_, err := changelogEntry(version)
	return err
}

// writeReleaseNotesFile materializes the GitHub release body — the R2 download
// header followed by the changelog entry — and returns its path.
func writeReleaseNotesFile(dir string, version releaseVersion, entry releasenotes.Entry) (string, error) {
	body := releaseNotesHeader(version, downloadBaseURL())
	if entry.Summary != "" {
		body += "\n" + entry.Summary + "\n"
	}
	body += "\n" + entry.Body + "\n"

	path := filepath.Join(dir, "release-notes-"+version.String()+".md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("write release notes: %w", err)
	}
	return path, nil
}

// changelogTargetVersion resolves the scaffold's argument: a channel name
// picks that channel's next version from the local tags, and anything else is
// taken as an explicit version. Tags alone are enough here — a draft entry does
// not need the live manifests that publishing validates against.
func changelogTargetVersion(ctx context.Context, arg string) (releaseVersion, error) {
	if !validChannel(arg) {
		version, err := parsePublishVersion(arg)
		if err != nil {
			return releaseVersion{}, fmt.Errorf("invalid version %q: %w", arg, err)
		}
		return version, nil
	}
	versions, err := releaseTagVersions(ctx)
	if err != nil {
		return releaseVersion{}, err
	}
	return parsePublishVersion(nextVersion(arg, versions))
}

// prTitleSuffix matches the "(#123)" a squashed pull request leaves on its
// subject line. The number is noise in a user-facing changelog, and its link
// would 404 for anyone outside this private repository.
var prTitleSuffix = regexp.MustCompile(`\s*\(#\d+\)$`)

// dependencyBump matches the subjects Renovate opens. They are real changes but
// not ones a user of the app has any use for reading about.
var dependencyBump = regexp.MustCompile(`^Update (dependency|module|.* to v)`)

// scaffoldChangelogEntry writes the entry for version, pre-filled with the
// commit subjects since the previous release tag. The point is that the author
// edits and groups prose rather than facing a blank page — the file is a draft,
// not the generated notes it replaced.
func scaffoldChangelogEntry(ctx context.Context, version releaseVersion) (string, error) {
	path := filepath.Join(changelogDir, version.String()+".md")
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	}

	subjects, err := commitSubjectsSincePreviousRelease(ctx, version)
	if err != nil {
		return "", err
	}
	if len(subjects) == 0 {
		subjects = []string{"Describe what changed."}
	}

	var body strings.Builder
	fmt.Fprintf(&body, "---\nversion: %s\ndate: %s\nsummary: \"\"\n---\n\n", version, time.Now().Format(time.DateOnly))
	for _, subject := range subjects {
		fmt.Fprintf(&body, "- %s\n", subject)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		return "", fmt.Errorf("write changelog entry: %w", err)
	}
	return path, nil
}

func commitSubjectsSincePreviousRelease(ctx context.Context, version releaseVersion) ([]string, error) {
	versions, err := releaseTagVersions(ctx)
	if err != nil {
		return nil, err
	}

	revisions := "HEAD"
	if previous, ok := previousReleaseVersion(version, versions); ok {
		revisions = "desktop-v" + previous.String() + "..HEAD"
	}
	output, err := commandOutput(ctx, "git", "log", "--no-merges", "--format=%s", revisions)
	if err != nil {
		return nil, fmt.Errorf("read commits since the previous release: %w", err)
	}

	var subjects []string
	for line := range strings.Lines(output) {
		subject := prTitleSuffix.ReplaceAllString(strings.TrimSpace(line), "")
		if subject == "" || dependencyBump.MatchString(subject) {
			continue
		}
		subjects = append(subjects, subject)
	}
	return subjects, nil
}

// previousReleaseVersion returns the greatest release below version, which is
// the baseline a new entry's commit range starts from.
func previousReleaseVersion(version releaseVersion, versions []releaseVersion) (releaseVersion, bool) {
	var previous releaseVersion
	found := false
	for _, candidate := range versions {
		if compareChannelRelease(candidate, version) >= 0 {
			continue
		}
		if !found || compareChannelRelease(candidate, previous) > 0 {
			previous, found = candidate, true
		}
	}
	return previous, found
}
