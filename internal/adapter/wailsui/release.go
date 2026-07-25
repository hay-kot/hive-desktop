package wailsui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

// RepoSlug is the GitHub owner/repo holding the desktop app's source.
// Release tags use the desktop-v<version> namespace, decoupled from the CLI's
// v<version> tags in colonyops/hive.
const RepoSlug = "hay-kot/hive-desktop"

// ShortCommit trims a git revision to its conventional 7-character short form,
// leaving shorter values (e.g. the "HEAD" default) untouched.
func ShortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

// RepoURL is the desktop app's GitHub repository home page. Unlike releaseURL
// it is always available, so the About screen can always link to the project.
func RepoURL() string {
	return fmt.Sprintf("https://github.com/%s", RepoSlug)
}

// ReleaseURL returns the GitHub tag page for a published desktop version, or
// "" when the version has no published release (dev builds, empty values,
// go-module pseudo-versions). The desktop-v tag is the source-side version
// anchor and changelog record (docs/decisions/0003); distribution and the
// updater themselves never read GitHub.
func ReleaseURL(version string) string {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if _, ok := ReleaseChannel(v); !ok {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/releases/tag/desktop-v%s", RepoSlug, v)
}

// releaseVersionRE matches the closed set of publishable versions enforced by
// cmd/release: X.Y.Z with an optional -dev.N / -beta.N
// prerelease identifier.
var releaseVersionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-(dev|beta)\.[0-9A-Za-z.]+)?$`)

// ReleaseChannel maps a published desktop version to its release channel
// (docs/decisions/0004): bare X.Y.Z → stable, X.Y.Z-beta.N → beta,
// X.Y.Z-dev.N → dev. ok is false for anything else — source builds ("dev"),
// "(devel)", pseudo-versions, and foreign prerelease identifiers — which also
// marks the version as unreleased.
func ReleaseChannel(version string) (channel string, ok bool) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	m := releaseVersionRE.FindStringSubmatch(v)
	if m == nil {
		return "", false
	}
	switch m[1] {
	case "dev":
		return settings.ChannelDev, true
	case "beta":
		return settings.ChannelBeta, true
	default:
		return settings.ChannelStable, true
	}
}
