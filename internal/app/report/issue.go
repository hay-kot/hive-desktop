package report

import (
	"fmt"
	"net/url"
	"strings"
)

// IssueFormURL is the public tracker's new-issue endpoint. The app bakes in
// the repository the same way it bakes in the product domain.
const IssueFormURL = "https://github.com/hay-kot/hive-desktop/issues/new"

// IssueURL returns the bug form with the build identity filled in.
//
// Build identity is the only thing prefilled. A bundle's other surfaces name
// the user's machine, hosts and repositories, and this URL opens a public
// issue — so they travel as a file the user reviews and attaches, never as
// query parameters the app sends on their behalf.
func IssueURL(b BuildInfo) string {
	q := url.Values{}
	q.Set("template", "bug.yml")
	q.Set("version", versionField(b))
	q.Set("environment", environmentField(b))
	return IssueFormURL + "?" + q.Encode()
}

// versionField matches the wording the template asks for: a released build
// reports its version and channel, an unreleased one reports how it was made.
func versionField(b BuildInfo) string {
	if b.Version == "" || b.Version == "dev" {
		return "built from source"
	}
	if b.Channel == "" {
		return b.Version
	}
	return fmt.Sprintf("%s (%s)", b.Version, b.Channel)
}

func environmentField(b BuildInfo) string {
	lines := []string{fmt.Sprintf("- %s/%s", b.OS, b.Arch)}
	if b.GoVersion != "" {
		lines = append(lines, "- "+b.GoVersion)
	}
	if b.Commit != "" {
		lines = append(lines, "- commit "+b.Commit)
	}
	return strings.Join(lines, "\n")
}
