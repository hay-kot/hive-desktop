/**
 * hivedesktop.com
 *
 * Static assets (the MkDocs build in site/) are served by the platform before
 * this Worker runs; it only sees the /api/* routes below and anything the
 * asset router did not match.
 */

// REPORTS/REPORT_TOKEN are optional: if either is absent the report endpoint
// answers 503 rather than accepting uploads.
export interface Env {
  ASSETS: Fetcher;
  REPORTS?: R2Bucket;
  REPORT_TOKEN?: string;
}

/** Release channel manifest the download CTA resolves through. */
const MANIFEST_URL =
  "https://dl.hivedesktop.com/desktop/channels/stable/latest.json";

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

const MAX_REPORT_BYTES = 5 * 1024 * 1024;
const REPORT_ID_PATTERN = /^rpt_[0-9a-f]{32}$/;
const META_MAX_LENGTH = 128;

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === "/api/latest") {
      return handleLatest(request);
    }

    if (url.pathname === "/api/report") {
      return handleReport(request, env);
    }

    const legacy = legacyTarget(url.pathname);
    if (legacy) {
      return Response.redirect(new URL(legacy, url).toString(), 301);
    }

    return env.ASSETS.fetch(request);
  },
} satisfies ExportedHandler<Env>;

/**
 * Same-origin proxy for the stable channel manifest. Fetching R2 directly from
 * the page would need CORS on dl.hivedesktop.com; proxying keeps the download
 * button on one origin and lets us cache at the edge.
 */
async function handleLatest(request: Request): Promise<Response> {
  if (request.method !== "GET" && request.method !== "HEAD") {
    return methodNotAllowed("GET, HEAD");
  }

  const upstream = await fetch(MANIFEST_URL, {
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

// Ingest a gzipped diagnostic bundle from the desktop app and store it in the
// private reports bucket. The object key is built here from the server clock,
// so a client cannot choose where its report lands.
export async function handleReport(request: Request, env: Env): Promise<Response> {
  if (request.method !== "POST") {
    return methodNotAllowed("POST");
  }

  if (!env.REPORT_TOKEN || !env.REPORTS) {
    return json({ error: "reporting_disabled" }, 503);
  }

  const presented = bearerToken(request.headers.get("authorization"));
  if (!timingSafeEqual(presented, env.REPORT_TOKEN)) {
    return json({ error: "unauthorized" }, 401);
  }

  if (request.headers.get("content-encoding") !== "gzip") {
    return json({ error: "gzip_required" }, 415);
  }

  const reportId = request.headers.get("x-hive-report-id") ?? "";
  if (!REPORT_ID_PATTERN.test(reportId)) {
    return json({ error: "invalid_report_id" }, 400);
  }

  // Enforce the cap from the declared length before buffering the body, so an
  // authenticated client cannot make the worker read an oversized payload into
  // the isolate.
  const declared = Number(request.headers.get("content-length"));
  if (!Number.isFinite(declared) || declared <= 0) {
    return json({ error: "length_required" }, 411);
  }
  if (declared > MAX_REPORT_BYTES) {
    return json({ error: "too_large" }, 413);
  }

  const body = new Uint8Array(await request.arrayBuffer());
  if (body.byteLength === 0) {
    return json({ error: "empty_body" }, 400);
  }
  if (body.byteLength > MAX_REPORT_BYTES) {
    return json({ error: "too_large" }, 413);
  }
  // gzip magic; the worker stores the bytes as-is and never decompresses.
  if (body[0] !== 0x1f || body[1] !== 0x8b) {
    return json({ error: "not_gzip" }, 400);
  }

  const now = new Date();
  const key = `reports/${objectDatePrefix(now)}/${reportId}.json.gz`;

  try {
    await env.REPORTS.put(key, body, {
      httpMetadata: { contentType: "application/json", contentEncoding: "gzip" },
      customMetadata: {
        reportId,
        receivedAt: now.toISOString(),
        version: metaHeader(request, "x-hive-version"),
        os: metaHeader(request, "x-hive-os"),
        arch: metaHeader(request, "x-hive-arch"),
      },
    });
  } catch (error) {
    console.error("report store failed", error);
    return json({ error: "store_failed" }, 502);
  }

  return json({ id: reportId });
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

function bearerToken(header: string | null): string {
  const prefix = "Bearer ";
  if (!header || !header.startsWith(prefix)) {
    return "";
  }
  return header.slice(prefix.length);
}

function timingSafeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) {
    return false;
  }
  let diff = 0;
  for (let i = 0; i < a.length; i++) {
    diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  }
  return diff === 0;
}

function objectDatePrefix(now: Date): string {
  const yyyy = String(now.getUTCFullYear()).padStart(4, "0");
  const mm = String(now.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(now.getUTCDate()).padStart(2, "0");
  return `${yyyy}/${mm}/${dd}`;
}

function metaHeader(request: Request, name: string): string {
  return (request.headers.get(name) ?? "")
    .slice(0, META_MAX_LENGTH)
    .replace(/[^\x20-\x7e]/g, "");
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
