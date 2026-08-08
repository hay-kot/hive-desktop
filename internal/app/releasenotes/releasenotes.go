// Package releasenotes serves the changelog that ships inside the binary:
// what changed in each stable release, what a build carries that no stable
// release has yet, and whether the user has seen either.
//
// The notes are embedded rather than fetched. The repository is private, so a
// user cannot read its GitHub releases, and the notes for the build you are
// running have to be readable at first launch, offline, before any network
// call can answer (ADR release-notes-ship-inside-the-binary).
//
// Only a stable release gets an entry of its own. Everything else accumulates
// in next.md, the draft every prerelease build embeds as "what is in this
// build that no stable release has" — which is why cutting a dev or beta
// release needs no changelog work at all, and why the entry a stable release
// needs is written by promoting a draft that already exists.
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

// DraftFile is the accumulating entry for work that has not reached a stable
// release. It carries no version because the release it describes has no
// number until promotion assigns one.
const DraftFile = "next.md"

// Entry is one set of release notes.
type Entry struct {
	// Version is the published stable version, without a v or desktop-v
	// prefix. It is empty on the draft.
	Version string
	// Date is the release date, zero on the draft.
	Date time.Time
	// Summary is an optional one-line description — what the What's New toast
	// shows, and what a channel manifest carries for a release the user has
	// not installed yet, where a full body does not fit.
	Summary string
	// Body is the markdown detail, which may be empty for a release whose
	// summary says everything.
	Body string
	// Draft marks the unreleased entry. At most one exists, it is dropped
	// entirely when empty, and it sorts above every release.
	Draft bool
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
		entry, err := parseFile(name, raw)
		if err != nil {
			return nil, err
		}
		// An empty draft is the state a promotion leaves behind, and it lasts
		// until the next change lands. Dropping it here is what keeps every
		// reader from having to ask whether the draft says anything.
		if entry.Draft && entry.Summary == "" && entry.Body == "" {
			continue
		}
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b Entry) int { return -compareEntries(a, b) })
	return entries, nil
})

func parseFile(filename string, raw []byte) (Entry, error) {
	if filename == DraftFile {
		return parseDraft(raw)
	}
	return parseEntry(filename, raw)
}

func compareEntries(a, b Entry) int {
	if a.Draft != b.Draft {
		if a.Draft {
			return 1
		}
		return -1
	}
	left, _ := parseVersion(a.Version)
	right, _ := parseVersion(b.Version)
	return compare(left, right)
}

// frontmatter is the YAML header a changelog entry carries. The draft uses
// summary alone; version and date are assigned when it is promoted.
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
	if parsed.prerelease != "" {
		return Entry{}, fmt.Errorf(
			"changelog %s: %q is a prerelease, and only a stable release gets an entry of its own; unreleased work belongs in %s",
			filename, fm.Version, DraftFile)
	}
	date, err := time.Parse(time.DateOnly, fm.Date)
	if err != nil {
		return Entry{}, fmt.Errorf("changelog %s: date must be YYYY-MM-DD: %w", filename, err)
	}

	return Entry{
		Version: fm.Version,
		Date:    date,
		Summary: strings.TrimSpace(fm.Summary),
		Body:    strings.TrimSpace(body),
	}, nil
}

// parseDraft reads next.md. Its header is optional and only summary is read
// from it, so landing a changelog line is appending a bullet to the file in
// the pull request that earns it.
func parseDraft(raw []byte) (Entry, error) {
	body := strings.ReplaceAll(string(raw), "\r\n", "\n")
	var fm frontmatter
	if strings.HasPrefix(body, "---\n") {
		header, rest, err := splitFrontmatter(raw)
		if err != nil {
			return Entry{}, fmt.Errorf("changelog %s: %w", DraftFile, err)
		}
		if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
			return Entry{}, fmt.Errorf("changelog %s: parse frontmatter: %w", DraftFile, err)
		}
		body = rest
	}
	return Entry{
		Draft:   true,
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

// Between returns what a launch on current should be shown, given after as the
// newest version whose notes have already been seen: the stable releases
// crossed, plus the draft, which describes work this build carries that no
// stable release does.
//
// An unparseable after — no recorded version — means no lower bound; an
// unparseable current means nothing to show, because a build outside the
// publishable set has no release to describe.
func (e Entries) Between(after, current string) Entries {
	to, ok := parseVersion(current)
	if !ok {
		return nil
	}
	from, hasFrom := parseVersion(after)

	out := make(Entries, 0, len(e))
	for _, entry := range e {
		if entry.Draft {
			out = append(out, entry)
			continue
		}
		v, ok := parseVersion(entry.Version)
		if !ok || compare(v, to) > 0 {
			continue
		}
		if hasFrom && compare(v, from) <= 0 {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// Find returns the entry describing version. The draft is never a match: it
// describes no version until it is promoted into one.
func (e Entries) Find(version string) (Entry, bool) {
	target, ok := parseVersion(version)
	if !ok {
		return Entry{}, false
	}
	for _, entry := range e {
		if entry.Draft {
			continue
		}
		if v, ok := parseVersion(entry.Version); ok && compare(v, target) == 0 {
			return entry, true
		}
	}
	return Entry{}, false
}

// Draft returns the accumulating entry for unreleased work. ok is false when
// nothing has landed since the last stable release.
func (e Entries) Draft() (Entry, bool) {
	for _, entry := range e {
		if entry.Draft {
			return entry, true
		}
	}
	return Entry{}, false
}
