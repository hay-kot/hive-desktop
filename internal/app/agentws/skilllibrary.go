package agentws

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// LibrarySkill is one user-authored skill in the library at
// <root>/.shared/skills/<slug>/SKILL.md. Body is the file verbatim: the
// library is authored in the format agents already read, so a workspace that
// enables the slug installs those exact bytes and nothing re-renders them.
type LibrarySkill struct {
	Slug        string
	Description string
	Body        string
}

// SkillsLibraryDir is the library directory under a workspace root.
func SkillsLibraryDir(root string) string {
	return filepath.Join(root, sharedDirName, skillsDirName)
}

// LoadSkillLibrary reads every <slug>/SKILL.md under dir, sorted by slug. A
// missing directory is an empty library rather than a failure. A directory
// carrying no SKILL.md, or named something no workspace could declare, is
// skipped: the library is a directory a user drops files into, so one stray
// entry must not empty the catalogue every workspace editor reads.
func LoadSkillLibrary(dir string) ([]LibrarySkill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("agentws: read skill library: %w", err)
	}

	out := make([]LibrarySkill, 0, len(entries))
	for _, entry := range entries {
		slug := entry.Name()
		if !entry.IsDir() || !validSlug(slug) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, slug, skillFileName))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("agentws: read skill %q: %w", slug, err)
		}
		out = append(out, LibrarySkill{
			Slug:        slug,
			Description: skillDescription(body),
			Body:        string(body),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// skillDescription reads the description out of a SKILL.md's YAML
// frontmatter, or "" when the file has none. A file with no frontmatter is
// still a usable skill — every agent reads the body — so an unparseable
// header costs the entry its catalogue description, never its place in the
// library.
func skillDescription(body []byte) string {
	const fence = "---\n"
	if !bytes.HasPrefix(body, []byte(fence)) {
		return ""
	}
	header, _, found := bytes.Cut(body[len(fence):], []byte("\n---"))
	if !found {
		return ""
	}
	var meta struct {
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal(header, &meta); err != nil {
		return ""
	}
	return meta.Description
}
