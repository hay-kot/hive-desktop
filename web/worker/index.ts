/**
 * hivedesktop.com
 *
 * Static assets (the MkDocs build in site/) are served by the platform before
 * this Worker runs; it only sees the /api/* routes below and anything the
 * asset router did not match.
 */

export interface Env {
  ASSETS: Fetcher;
}

/** Release channel manifests the download CTA resolves through. */
const CHANNELS_BASE = "https://dl.hivedesktop.com/desktop/channels";

/**
 * A `?channel=` value is interpolated into the upstream URL, so it is matched
 * against this set rather than sanitized. Stable is the default because that is
 * what an unqualified "latest" means once stable exists; the download page asks
 * for another channel while it does not.
 */
const CHANNELS = new Set(["stable", "beta", "dev"]);
const DEFAULT_CHANNEL = "stable";

/** Manifests are written with `no-cache`; a short edge TTL keeps the CTA fresh. */
const MANIFEST_TTL_SECONDS = 300;

/**
 * The site's URLs changed when it became a Zensical site: the docs lost their
 * /docs prefix and were regrouped, and the install and compare pages went
 * away. The installed app's About pane still links /docs and
 * /docs/help/updates, and the README linked /install. Redirected here rather
 * than by a _redirects file, which the platform does not apply to requests
 * this Worker answers.
 */
const LEGACY_DOCS = /^\/docs(?:\/(.*))?$/;
const LEGACY_PAGES: Record<string, string> = {
  "/install": "/getting-started/#install",
  "/compare": "/",
};
const MOVED_DOCS: Record<string, string> = {
  "concepts/how-it-works": "/inbox/how-it-works/",
  "concepts/flows": "/inbox/flows/",
  "concepts/sources": "/inbox/sources/",
  "concepts/actions": "/inbox/actions/",
  "concepts/terminal-mode": "/code/terminal-mode/",
  "concepts/agent-workspaces": "/chats/agent-workspaces/",
  "help/troubleshooting": "/getting-started/troubleshooting/",
  "help/reporting-a-problem": "/getting-started/troubleshooting/#report-a-problem",
  "help/updates": "/configuration/settings/#updates",
};

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === "/api/latest") {
      return handleLatest(request, url);
    }

    const legacy = legacyTarget(url.pathname);
    if (legacy) {
      return Response.redirect(new URL(legacy, url).toString(), 301);
    }

    return env.ASSETS.fetch(request);
  },
} satisfies ExportedHandler<Env>;

/**
 * Same-origin proxy for a channel manifest. Fetching R2 directly from the page
 * would need CORS on dl.hivedesktop.com; proxying keeps the download buttons on
 * one origin and lets us cache at the edge. The body is passed through
 * untouched, so the manifest's own `channel` field tells the page what it got.
 */
async function handleLatest(request: Request, url: URL): Promise<Response> {
  if (request.method !== "GET" && request.method !== "HEAD") {
    return methodNotAllowed("GET, HEAD");
  }

  const channel = url.searchParams.get("channel") ?? DEFAULT_CHANNEL;
  if (!CHANNELS.has(channel)) {
    return json({ error: "unknown_channel" }, 400);
  }

  const upstream = await fetch(`${CHANNELS_BASE}/${channel}/latest.json`, {
    cf: { cacheTtl: MANIFEST_TTL_SECONDS, cacheEverything: true },
  });

  if (!upstream.ok) {
    return json({ error: "manifest_unavailable" }, 502);
  }

  const body = await upstream.text();

  return new Response(body, {
    headers: {
      "content-type": "application/json; charset=utf-8",
      "cache-control": `public, max-age=60, s-maxage=${MANIFEST_TTL_SECONDS}`,
    },
  });
}

/**
 * /docs goes to the overview, a moved page to its new home, and any other
 * /docs/<path> to /<path>/. /install and /compare* go where their content went.
 */
function legacyTarget(pathname: string): string | null {
  const bare = pathname.replace(/\/+$/, "");
  if (bare in LEGACY_PAGES) {
    return LEGACY_PAGES[bare];
  }
  if (bare.startsWith("/compare/")) {
    return "/";
  }
  const docs = LEGACY_DOCS.exec(pathname);
  if (!docs) {
    return null;
  }
  const path = (docs[1] ?? "").replace(/\/+$/, "");
  if (path === "") {
    return "/getting-started/";
  }
  return MOVED_DOCS[path] ?? `/${path}/`;
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "content-type": "application/json; charset=utf-8",
      "cache-control": "no-store",
    },
  });
}

function methodNotAllowed(allow: string): Response {
  return new Response(null, { status: 405, headers: { allow } });
}
