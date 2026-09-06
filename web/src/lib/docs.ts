import type { CollectionEntry } from "astro:content";

/** Sidebar group order. A page's `group` frontmatter must be one of these. */
export const DOC_GROUPS = ["Getting started", "Concepts", "Configuration", "Help"] as const;
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

/** The Markdown twin of a page, served for LLMs and for "view source". */
export function docMarkdownHref(entry: CollectionEntry<"docs">): string {
  return `/docs/${entry.id}.md`;
}

/**
 * A page's body as standalone Markdown: the rendered title and lede restored
 * as a heading and a blockquote (the layout draws them from frontmatter, so the
 * body starts at `##`), and root-relative links made absolute so the text
 * reads correctly outside the site.
 */
export function docMarkdown(entry: CollectionEntry<"docs">, site: URL): string {
  const body = (entry.body ?? "").replace(
    /(\]\()\/(?!\/)/g,
    (_match, open: string) => `${open}${site.origin}/`,
  );
  return [
    `# ${entry.data.title}`,
    "",
    `> ${entry.data.description}`,
    "",
    body.trim(),
    "",
  ].join("\n");
}
