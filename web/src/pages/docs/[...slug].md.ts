import type { APIRoute } from "astro";
import { getCollection } from "astro:content";

import { docMarkdown } from "../../lib/docs";

/**
 * Each docs page as Markdown at its own URL plus `.md` (the section root is
 * `/docs/index.md`). This is what llms.txt links to, and what a reader who
 * wants the source instead of the rendered page gets.
 */
export async function getStaticPaths() {
  const entries = await getCollection("docs", ({ data }) => !data.draft);
  return entries.map((entry) => ({ params: { slug: entry.id }, props: { entry } }));
}

export const GET: APIRoute = ({ props, site }) => {
  return new Response(docMarkdown(props.entry, site!), {
    headers: { "Content-Type": "text/markdown; charset=utf-8" },
  });
};
