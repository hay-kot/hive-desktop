import type { CollectionEntry } from "astro:content";

/** Sidebar group order. A page's `group` frontmatter must be one of these. */
export const DOC_GROUPS = ["Getting started", "Concepts", "Help"] as const;
export type DocGroup = (typeof DOC_GROUPS)[number];

export type DocsTree = { group: DocGroup; entries: CollectionEntry<"docs">[] }[];

/** Groups pages for the sidebar, ordered by DOC_GROUPS then `order`, dropping empty groups. */
export function buildDocsTree(entries: CollectionEntry<"docs">[]): DocsTree {
  return DOC_GROUPS.map((group) => ({
    group,
    entries: entries
      .filter((entry) => entry.data.group === group)
      .sort(
        (a, b) =>
          a.data.order - b.data.order || a.data.title.localeCompare(b.data.title),
      ),
  })).filter((group) => group.entries.length > 0);
}

/** `index` is the section root at /docs; every other page nests under it. */
export function docHref(entry: CollectionEntry<"docs">): string {
  return entry.id === "index" ? "/docs" : `/docs/${entry.id}`;
}
