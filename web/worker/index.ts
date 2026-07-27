/**
 * hivedesktop.com
 *
 * Static assets (the Astro build in dist/) are served by the platform before
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
 * The listmonk instance that runs the mailing list. Subscriptions are
 * forwarded to its public form endpoint; `l` is the Hive Desktop list UUID.
 */
const LISTMONK_FORM_URL = "https://listmonk.haybytes.com/subscription/form";
const LISTMONK_LIST_UUID = "ae24f0b5-c230-4d2e-9fc0-747e9270636e";

const MAX_EMAIL_LENGTH = 254;
const EMAIL_PATTERN = /^[^\s@]+@[^\s@.]+(\.[^\s@.]+)+$/;

const MAX_REPORT_BYTES = 5 * 1024 * 1024;
const REPORT_ID_PATTERN = /^rpt_[0-9a-f]{32}$/;
const META_MAX_LENGTH = 128;

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === "/api/latest") {
      return handleLatest(request);
    }

    if (url.pathname === "/api/subscribe") {
      return handleSubscribe(request);
    }

    if (url.pathname === "/api/report") {
      return handleReport(request, env);
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

/** Private-beta invite requests, forwarded to listmonk. */
async function handleSubscribe(request: Request): Promise<Response> {
  if (request.method !== "POST") {
    return methodNotAllowed("POST");
  }

  let payload: unknown;
  try {
    payload = await request.json();
  } catch {
    return json({ error: "invalid_body" }, 400);
  }

  if (typeof payload !== "object" || payload === null) {
    return json({ error: "invalid_body" }, 400);
  }

  const { email, company } = payload as Record<string, unknown>;

  // Honeypot: the form renders `company` hidden, so only bots fill it in.
  // Answer 200 so they cannot distinguish a trap from a success.
  if (typeof company === "string" && company.trim() !== "") {
    return json({ ok: true });
  }

  if (typeof email !== "string") {
    return json({ error: "invalid_email" }, 400);
  }

  const normalized = email.trim().toLowerCase();
  if (normalized.length > MAX_EMAIL_LENGTH || !EMAIL_PATTERN.test(normalized)) {
    return json({ error: "invalid_email" }, 400);
  }

  // Listmonk's public form endpoint. `nonce` is its own honeypot and must be
  // sent present-but-empty, exactly as its rendered form would.
  let upstream: Response;
  try {
    upstream = await fetch(LISTMONK_FORM_URL, {
      method: "POST",
      headers: { "content-type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ email: normalized, l: LISTMONK_LIST_UUID, nonce: "" }),
    });
  } catch (error) {
    console.error("subscribe forward failed", error);
    return json({ error: "subscribe_failed" }, 502);
  }

  if (upstream.status === 400) {
    return json({ error: "invalid_email" }, 400);
  }

  if (!upstream.ok) {
    console.error("listmonk rejected subscription", upstream.status);
    return json({ error: "subscribe_failed" }, 502);
  }

  return json({ ok: true });
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

  const declared = Number(request.headers.get("content-length"));
  if (Number.isFinite(declared) && declared > MAX_REPORT_BYTES) {
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
