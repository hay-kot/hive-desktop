package agentws

import "sort"

// ShippedSkill is one skill this build ships, as the catalogue needs it. The
// shipped half arrives as data rather than being read here: the bodies are
// rendered per install by the prompt engine, and internal/app/prompts imports
// this package, so the dependency cannot run the other way.
type ShippedSkill struct {
	Slug        string
	Title       string
	Description string
}

// SkillPackageEntry is one row of the package catalogue — a package as the
// workspace editor shows it, with the names it currently resolves to. Members
// are resolved for display rather than declared: a package is patterns, so
// what it contains is a question only the current name-space can answer.
type SkillPackageEntry struct {
	Name        string
	Title       string
	Description string
	Members     []SkillName
}

// SkillNames is the name-space packages glob over: every shipped skill plus
// every skill authored in the shared directory, sorted, with a shared skill
// of the same name winning — a slug is one skill, and the authored copy is
// the one the user can see and edit.
func SkillNames(shipped []ShippedSkill, sharedSkills []SharedSkill) []SkillName {
	names := make(map[string]SkillName, len(shipped)+len(sharedSkills))
	for _, s := range shipped {
		names[s.Slug] = SkillName{Slug: s.Slug, Shipped: true}
	}
	for _, s := range sharedSkills {
		names[s.Slug] = SkillName{Slug: s.Slug}
	}

	out := make([]SkillName, 0, len(names))
	for _, name := range names {
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// SkillPackageCatalogue lists every defined package with the names it
// resolves to, sorted by package name. A package matching nothing is listed
// with no members rather than hidden: an empty package is a pattern to fix,
// and hiding it would make skills.yml and the editor disagree.
func SkillPackageCatalogue(lib SkillLibrary, names []SkillName) []SkillPackageEntry {
	out := make([]SkillPackageEntry, 0, len(lib.Packages))
	for id, pkg := range lib.Packages {
		title := pkg.Title
		if title == "" {
			title = id
		}
		out = append(out, SkillPackageEntry{
			Name:        id,
			Title:       title,
			Description: pkg.Description,
			Members:     pkg.Members(names),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
