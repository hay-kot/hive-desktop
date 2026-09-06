import type { APIRoute } from "astro";
import { getCollection } from "astro:content";

import { site } from "../data";
import { buildDocsTree, docMarkdownHref } from "../lib/docs";

/**
 * The llms.txt index (https://llmstxt.org): one link per page, grouped the way
 * the sidebar is, pointing at each page's Markdown twin. llms-full.txt is the
 * same set inlined.
 */
export const GET: APIRoute = async ({ site: siteURL }) => {
  const docs = await getCollection("docs", ({ data }) => !data.draft);
  const tree = buildDocsTree(docs);

  const sections = tree.map((group) => [
    `## ${group.group}`,
    "",
    ...group.entries.map(
      (entry) =>
        `- [${entry.data.title}](${new URL(docMarkdownHref(entry), siteURL)}): ${entry.data.description}`,
    ),
    "",
  ]);

  const lines = [
    `# ${site.brand.name} Desktop`,
    "",
    `> ${site.description}`,
    "",
    "Hive Desktop is a macOS app that pulls pull requests, issues, notifications,",
    "alerts, and webhook deliveries into local feeds, routes them through flows",
    "you configure as YAML, and can hand an item to a coding agent or run a",
    "command on it. Every page below is also served as plain Markdown at the",
    "linked URL; the HTML page is the same path without the `.md` suffix.",
    "",
    ...sections.flat(),
    "## Optional",
    "",
    `- [Full documentation in one file](${new URL("/llms-full.txt", siteURL)}): every page above, concatenated`,
    `- [Source repository](https://github.com/hay-kot/hive-desktop): the app, the docs source, and the issue tracker`,
    "",
  ];

  return new Response(lines.join("\n"), {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};
