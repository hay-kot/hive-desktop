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

// SkillCatalogueEntry is one row of the merged skill catalogue: a shipped
// skill, a library skill, or a library skill that replaces a shipped one.
type SkillCatalogueEntry struct {
	Slug        string
	Title       string
	Description string
	Shipped     bool
	// Shadows is the shipped slug this library skill replaces, empty
	// otherwise.
	Shadows string
}

// SkillCatalogue merges the shipped set with the user library into one list
// sorted by slug. A library slug shadowing a shipped one wins and says so —
// the same rule mcps.yaml follows against the shipped MCP registry
// (Catalogue), stated once for both halves of a workspace's capability
// declaration. Nothing here is enabled by being present: a workspace carries
// a skill only by naming its slug in skills:.
func SkillCatalogue(shipped []ShippedSkill, library []LibrarySkill) []SkillCatalogueEntry {
	entries := make(map[string]SkillCatalogueEntry, len(shipped)+len(library))
	for _, s := range shipped {
		entries[s.Slug] = SkillCatalogueEntry{
			Slug:        s.Slug,
			Title:       s.Title,
			Description: s.Description,
			Shipped:     true,
		}
	}

	for _, s := range library {
		entry := SkillCatalogueEntry{Slug: s.Slug, Title: s.Slug, Description: s.Description}
		if _, ok := entries[s.Slug]; ok {
			entry.Shadows = s.Slug
		}
		entries[s.Slug] = entry
	}

	out := make([]SkillCatalogueEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}
