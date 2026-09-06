import type { APIRoute } from "astro";
import { getCollection } from "astro:content";

import { site } from "../data";
import { buildDocsTree, docHref, docMarkdown } from "../lib/docs";

/** Every docs page as Markdown in sidebar order, for an LLM to read in one go. */
export const GET: APIRoute = async ({ site: siteURL }) => {
  const docs = await getCollection("docs", ({ data }) => !data.draft);
  const pages = buildDocsTree(docs).flatMap((group) => group.entries);

  const parts = pages.map((entry) =>
    [
      "---",
      "",
      `<!-- ${new URL(docHref(entry), siteURL)} · ${entry.data.group} -->`,
      "",
      docMarkdown(entry, siteURL!),
    ].join("\n"),
  );

  const text = [
    `# ${site.brand.name} Desktop documentation`,
    "",
    `> ${site.description}`,
    "",
    `Every page of ${new URL("/docs", siteURL)}, in sidebar order. The index with one link per page is at ${new URL("/llms.txt", siteURL)}.`,
    "",
    ...parts,
    "",
  ].join("\n");

  return new Response(text, {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};
