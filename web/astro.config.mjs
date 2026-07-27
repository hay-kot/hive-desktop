// @ts-check
import { defineConfig } from "astro/config";

// Static output only — the site is served as Cloudflare Worker static assets
// (see wrangler.jsonc). The Worker in worker/ handles the two /api/* routes and
// falls through to these assets for everything else.
export default defineConfig({
  site: "https://hivedesktop.com",
  outDir: "./dist",
  build: {
    assets: "_assets",
  },
});
