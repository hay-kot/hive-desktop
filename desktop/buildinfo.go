package main

import (
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/desktop"
)

// Build information for the desktop app. Populated at build time via
// -ldflags "-X main.version=... -X main.commit=... -X main.date=...". The
// production build in desktop/build/darwin/Taskfile.yml and the release
// pipeline (scripts/release/release-desktop.sh, wrapped by the
// desktop-publish workflow) stamp the release version, commit SHA, and build
// date here so the running app can report exactly what it is.
//
// A plain source build reports "dev".
var (
	version = "dev"
	commit  = "HEAD"
	date    = "now"
)

// desktopRepoSlug is the GitHub owner/repo holding the desktop app's source.
// Release tags use the desktop-v<version> namespace, decoupled from the CLI's
// v<version> tags in colonyops/hive.
const desktopRepoSlug = "hay-kot/hive-desktop"

// resolvedBuildInfo returns the effective version, commit, and date for the
// running binary. When ldflags were not supplied (a plain `go build`/`go run`,
// where version is still "dev") it falls back to the module + VCS metadata Go
// records automatically, mirroring the CLI's resolvedBuildInfo.
func resolvedBuildInfo() (v, c, d string) {
	v, c, d = version, commit, date
	if v != "dev" {
		return v, c, d
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v, c, d
	}
	if mv := info.Main.Version; mv != "" && mv != "(devel)" {
		v = mv
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			c = s.Value
		case "vcs.time":
			d = s.Value
		}
	}
	return v, c, d
}

// shortCommit trims a git revision to its conventional 7-character short form,
// leaving shorter values (e.g. the "HEAD" default) untouched.
func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

// repoURL is the desktop app's GitHub repository home page. Unlike releaseURL
// it is always available, so the About screen can always link to the project.
func repoURL() string {
	return fmt.Sprintf("https://github.com/%s", desktopRepoSlug)
}

// releaseURL returns the GitHub tag page for a published desktop version, or
// "" when the version has no published release (dev builds, empty values,
// go-module pseudo-versions). The desktop-v tag is the source-side version
// anchor and changelog record (docs/decisions/0003); distribution and the
// updater themselves never read GitHub.
func releaseURL(version string) string {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if _, ok := releaseChannel(v); !ok {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/releases/tag/desktop-v%s", desktopRepoSlug, v)
}

// releaseVersionRE matches the closed set of publishable versions enforced by
// scripts/release/release-desktop.sh: X.Y.Z with an optional -dev.N / -beta.N
// prerelease identifier.
var releaseVersionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-(dev|beta)\.[0-9A-Za-z.]+)?$`)

// releaseChannel maps a published desktop version to its release channel
// (docs/decisions/0004): bare X.Y.Z → stable, X.Y.Z-beta.N → beta,
// X.Y.Z-dev.N → dev. ok is false for anything else — source builds ("dev"),
// "(devel)", pseudo-versions, and foreign prerelease identifiers — which also
// marks the version as unreleased.
func releaseChannel(version string) (channel string, ok bool) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	m := releaseVersionRE.FindStringSubmatch(v)
	if m == nil {
		return "", false
	}
	switch m[1] {
	case "dev":
		return desktop.ChannelDev, true
	case "beta":
		return desktop.ChannelBeta, true
	default:
		return desktop.ChannelStable, true
	}
}
