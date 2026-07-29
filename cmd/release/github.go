package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// publishGitHubRelease records a published version on GitHub: it creates and
// pushes the lightweight desktop-v<version> tag and a GitHub Release whose notes
// capture the commits since the previous desktop release tag. This is the
// source-side record only — R2 remains the artifact store (decision 0003), so no
// binaries are attached. It is idempotent: an existing tag or release at the
// release commit is left untouched, so it can be re-run to recover a publish
// whose GitHub step failed after the irreversible R2 upload.
//
// The caller must have already validated that HEAD is the release commit
// (validatePublishSource); the tag is created there.
func publishGitHubRelease(ctx context.Context, version releaseVersion) error {
	tag := "desktop-v" + version.String()
	commit, err := gitHead(ctx)
	if err != nil {
		return err
	}
	if err := ensureLocalTag(ctx, tag, commit); err != nil {
		return err
	}
	if err := ensureOriginTag(ctx, tag, commit); err != nil {
		return err
	}
	exists, err := gitHubReleaseExists(ctx, tag)
	if err != nil {
		return err
	}
	if exists {
		fmt.Printf("==> GitHub release %s already exists; leaving it unchanged\n", tag)
		return nil
	}
	return createGitHubRelease(ctx, version, tag)
}

func ensureLocalTag(ctx context.Context, tag, commit string) error {
	exists, err := localTagExists(ctx, tag)
	if err != nil {
		return err
	}
	if exists {
		at, err := commandOutput(ctx, "git", "rev-list", "-n", "1", tag)
		if err != nil {
			return err
		}
		if strings.TrimSpace(at) != commit {
			return fmt.Errorf("local tag %s points at %s, not the release commit %s", tag, strings.TrimSpace(at), commit)
		}
		return nil
	}
	fmt.Printf("==> tagging %s\n", tag)
	return runCommand(ctx, "git", "tag", tag, commit)
}

func ensureOriginTag(ctx context.Context, tag, commit string) error {
	sha, err := originTagCommit(ctx, tag)
	if err != nil {
		return err
	}
	if sha != "" {
		if sha != commit {
			return fmt.Errorf("origin tag %s points at %s, not the release commit %s", tag, sha, commit)
		}
		fmt.Printf("==> origin already has tag %s\n", tag)
		return nil
	}
	fmt.Printf("==> pushing tag %s to origin\n", tag)
	return runCommand(ctx, "git", "push", "origin", "refs/tags/"+tag)
}

// originTagCommit returns the commit a tag points at on origin, or "" when the
// tag is absent. desktop-v* tags are lightweight, so ls-remote reports the
// commit directly rather than a tag object.
func originTagCommit(ctx context.Context, tag string) (string, error) {
	output, err := commandOutput(ctx, "git", "ls-remote", "--tags", "origin", "refs/tags/"+tag)
	if err != nil {
		return "", fmt.Errorf("check origin tag %s: %w", tag, err)
	}
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

func gitHubReleaseExists(ctx context.Context, tag string) (bool, error) {
	output, err := exec.CommandContext(ctx, "gh", "release", "view", tag, "--json", "tagName").CombinedOutput()
	if err == nil {
		return true, nil
	}
	// gh returns a non-zero exit both when the release is missing and on real
	// failures (auth, network), so the message is the only signal.
	if strings.Contains(string(output), "release not found") {
		return false, nil
	}
	exitErr := new(exec.ExitError)
	if errors.As(err, &exitErr) {
		return false, fmt.Errorf("gh release view %s: %s", tag, strings.TrimSpace(string(output)))
	}
	return false, fmt.Errorf("gh release view %s: %w", tag, err)
}

func createGitHubRelease(ctx context.Context, version releaseVersion, tag string) error {
	previous, err := previousOriginReleaseTag(ctx, version)
	if err != nil {
		return err
	}
	prerelease := version.channel() != "stable"
	fmt.Printf("==> creating GitHub release %s (prerelease=%t, notes since %s)\n", tag, prerelease, previousLabel(previous))

	args := []string{
		"release", "create", tag,
		"--verify-tag",
		"--title", releaseTitle(version),
		"--notes", releaseNotesHeader(version, downloadBaseURL()),
		"--generate-notes",
	}
	if previous != "" {
		args = append(args, "--notes-start-tag", previous)
	}
	if prerelease {
		// dev and beta builds never sit above a shipped stable on the releases
		// page; only a stable release is "Latest".
		args = append(args, "--prerelease", "--latest=false")
	}
	return runCommand(ctx, "gh", args...)
}

func releaseTitle(version releaseVersion) string {
	return "Hive Desktop " + version.String()
}

// releaseNotesHeader is prepended to GitHub's generated commit/PR notes. It
// states plainly that downloads come from R2, not this release's assets.
func releaseNotesHeader(version releaseVersion, downloadBase string) string {
	prefix := fmt.Sprintf("%s/desktop/releases/%s", downloadBase, version)
	return fmt.Sprintf(
		"Downloads are served from Cloudflare R2, not GitHub (decision 0003); this release records the source changes for the %s channel.\n\n"+
			"- Artifacts: %s/\n"+
			"- Checksums: %s/SHA256SUMS\n",
		version.channel(), prefix, prefix,
	)
}

// previousOriginReleaseTag returns the tag to start generated notes from: the
// greatest desktop-v* tag below version that is present on origin. It returns ""
// when there is no earlier tag or the earlier tag was never pushed (the tags
// predating this flow), letting GitHub pick its own baseline.
func previousOriginReleaseTag(ctx context.Context, version releaseVersion) (string, error) {
	versions, err := releaseTagVersions(ctx)
	if err != nil {
		return "", err
	}
	previous, ok := previousReleaseVersion(version, versions)
	if !ok {
		return "", nil
	}
	tag := "desktop-v" + previous.String()
	sha, err := originTagCommit(ctx, tag)
	if err != nil {
		return "", err
	}
	if sha == "" {
		return "", nil
	}
	return tag, nil
}

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

func previousLabel(tag string) string {
	if tag == "" {
		return "the first release"
	}
	return tag
}
