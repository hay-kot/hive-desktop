import type { APIRoute } from "astro";
import { getCollection } from "astro:content";

import { comparisons } from "../data/compare";
import { docHref } from "../lib/docs";

/**
 * Hand-rolled rather than @astrojs/sitemap: outside the docs collection there
 * are only the landing page, the install page, and the comparison pages.
 */
export const GET: APIRoute = async ({ site }) => {
  const docs = await getCollection("docs", ({ data }) => !data.draft);
  const paths = [
    "/",
    "/install",
    "/compare",
    ...comparisons.map((entry) => `/compare/${entry.slug}`),
    ...docs.map(docHref),
  ];
  const urls = paths.map((path) => `  <url><loc>${new URL(path, site)}</loc></url>`);

  return new Response(
    [
      '<?xml version="1.0" encoding="UTF-8"?>',
      '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">',
      ...urls,
      "</urlset>",
      "",
    ].join("\n"),
    { headers: { "Content-Type": "application/xml" } },
  );
};
