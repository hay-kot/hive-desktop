// Package releasenotes serves the changelog that ships inside the binary:
// what changed in each published release, which of those releases a user
// crossed to reach the build they are running, and whether they have seen
// those notes yet.
//
// The notes are embedded rather than fetched. The repository is private, so a
// user cannot read its GitHub releases, and the notes for the build you are
// running have to be readable at first launch, offline, before any network
// call can answer (ADR release-notes-ship-inside-the-binary). That is also why
// an entry has to be authored before the release commit rather than generated
// at publish time — cmd/release refuses to publish a version with no entry.
package releasenotes

import (
	"embed"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed changelog/*.md
var changelogFS embed.FS

const changelogDir = "changelog"

// Entry is one published release's notes.
type Entry struct {
	// Version is the published version, without a v or desktop-v prefix.
	Version string
	Date    time.Time
	// Channel is the channel that published this release, derived from
	// Version rather than stored — the version string is what routes a
	// release (ADR release-channels).
	Channel string
	// Summary is an optional one-line description. It is what a toast and the
	// update-available chip show, where a full body does not fit.
	Summary string
	// Body is the markdown detail, which may be empty for a release whose
	// summary says everything.
	Body string
}

// Entries is a set of release notes ordered newest first.
type Entries []Entry

// Load parses the embedded changelog once per process. An error means a
// malformed entry was committed, which TestChangelogParses and the release
// gate both catch before it can ship.
var Load = sync.OnceValues(func() (Entries, error) {
	files, err := changelogFS.ReadDir(changelogDir)
	if err != nil {
		return nil, fmt.Errorf("read changelog: %w", err)
	}
	entries := make(Entries, 0, len(files))
	for _, file := range files {
		name := file.Name()
		raw, err := changelogFS.ReadFile(path.Join(changelogDir, name))
		if err != nil {
			return nil, fmt.Errorf("read changelog %s: %w", name, err)
		}
		entry, err := parseEntry(name, raw)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b Entry) int { return -compareEntries(a, b) })
	return entries, nil
})

func compareEntries(a, b Entry) int {
	left, _ := parseVersion(a.Version)
	right, _ := parseVersion(b.Version)
	return compare(left, right)
}

// frontmatter is the YAML header every changelog entry carries.
type frontmatter struct {
	Version string `yaml:"version"`
	Date    string `yaml:"date"`
	Summary string `yaml:"summary"`
}

// parseEntry reads one `<version>.md`. The filename is checked against the
// version in the header so a copy-pasted entry cannot silently describe the
// wrong release.
func parseEntry(filename string, raw []byte) (Entry, error) {
	header, body, err := splitFrontmatter(raw)
	if err != nil {
		return Entry{}, fmt.Errorf("changelog %s: %w", filename, err)
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
		return Entry{}, fmt.Errorf("changelog %s: parse frontmatter: %w", filename, err)
	}

	stem := strings.TrimSuffix(filename, ".md")
	if fm.Version != stem {
		return Entry{}, fmt.Errorf("changelog %s: version %q does not match its filename", filename, fm.Version)
	}
	parsed, ok := parseVersion(fm.Version)
	if !ok {
		return Entry{}, fmt.Errorf("changelog %s: %q is not a publishable version", filename, fm.Version)
	}
	date, err := time.Parse(time.DateOnly, fm.Date)
	if err != nil {
		return Entry{}, fmt.Errorf("changelog %s: date must be YYYY-MM-DD: %w", filename, err)
	}

	return Entry{
		Version: fm.Version,
		Date:    date,
		Channel: parsed.channel(),
		Summary: strings.TrimSpace(fm.Summary),
		Body:    strings.TrimSpace(body),
	}, nil
}

func splitFrontmatter(raw []byte) (header, body string, err error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", "", fmt.Errorf("missing --- frontmatter header")
	}
	header, after, found := strings.Cut(text[len("---\n"):], "\n---")
	if !found {
		return "", "", fmt.Errorf("unterminated frontmatter header")
	}
	return header, strings.TrimPrefix(after, "\n"), nil
}

// VisibleIn narrows to the releases a user following channel actually
// receives, so a stable user is never shown the dev builds that preceded their
// upgrade.
func (e Entries) VisibleIn(channel string) Entries {
	out := make(Entries, 0, len(e))
	for _, entry := range e {
		parsed, ok := parseVersion(entry.Version)
		if ok && visibleIn(parsed, channel) {
			out = append(out, entry)
		}
	}
	return out
}

// Between returns the releases a user crossed moving from after up to and
// including current, narrowed to channel. An unparseable after — no recorded
// version — means no lower bound; an unparseable current means nothing to
// show, because an unreleased build has no notes.
func (e Entries) Between(after, current, channel string) Entries {
	to, ok := parseVersion(current)
	if !ok {
		return nil
	}
	from, hasFrom := parseVersion(after)

	out := make(Entries, 0, len(e))
	for _, entry := range e {
		v, ok := parseVersion(entry.Version)
		if !ok || !visibleIn(v, channel) || compare(v, to) > 0 {
			continue
		}
		if hasFrom && compare(v, from) <= 0 {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// Find returns the entry describing version.
func (e Entries) Find(version string) (Entry, bool) {
	target, ok := parseVersion(version)
	if !ok {
		return Entry{}, false
	}
	for _, entry := range e {
		if v, ok := parseVersion(entry.Version); ok && compare(v, target) == 0 {
			return entry, true
		}
	}
	return Entry{}, false
}
