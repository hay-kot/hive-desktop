import type { APIRoute } from "astro";
import { getCollection } from "astro:content";

import { docHref } from "../lib/docs";

/**
 * Hand-rolled rather than @astrojs/sitemap: the only route outside the docs
 * collection is the landing page and the invite-gated installer, and that
 * installer must never be listed — robots.txt disallows /install/, and a
 * generated sitemap would publish the obscure path it is hidden behind.
 */
export const GET: APIRoute = async ({ site }) => {
  const docs = await getCollection("docs", ({ data }) => !data.draft);
  const paths = ["/", ...docs.map(docHref)];
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
