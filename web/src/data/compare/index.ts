/**
 * Comparison pages make factual claims about products that ship faster than
 * this site does, so the schema enforces the things that keep such a page
 * honest as it ages rather than leaving them to an author's discretion:
 *
 *  - `theirWins` and `chooseThem` have minimums, so a page cannot be published
 *    without conceding ground.
 *  - `rows` must contain at least three cells Hive loses, so the feature table
 *    cannot be stacked.
 *  - A `no` in a competitor's column needs a `source`. Absence-of-feature
 *    claims are the ones that rot into unfairness first.
 *  - `verifiedAt` and `theirVersion` are required and rendered, so a stale page
 *    degrades into "accurate as of" instead of silently lying.
 *
 * Durable arguments go in prose; volatile facts go in these files. Star counts,
 * funding and forum scores are deliberately absent — they are wrong within
 * weeks, and a stale low number for a competitor reads worse than no number.
 */
import { z } from "astro/zod";

import claudeCodeJson from "./claude-code.json";
import githubNotificationsJson from "./github-notifications.json";
import herdrJson from "./herdr.json";
import hubJson from "./hub.json";
import octoboxJson from "./octobox.json";

/** How a product fares on one row of the feature table. */
const cell = z.object({
  value: z.enum(["yes", "no", "partial", "n/a"]),
  note: z.string().optional(),
  /** Required when a competitor's cell is `no` — see the module comment. */
  source: z.string().url().optional(),
});

const row = z
  .object({
    label: z.string(),
    them: cell,
    us: cell,
  })
  .refine((entry) => entry.them.value !== "no" || Boolean(entry.them.source), {
    message: "a `no` in the competitor column needs a `source` URL",
    path: ["them", "source"],
  });

const claim = z.object({ title: z.string(), body: z.string() });

export const comparisonSchema = z
  .object({
    slug: z.string(),
    /** Drives the page title and every "vs X" string on it. */
    them: z.object({
      name: z.string(),
      url: z.string().url(),
      /**
       * Their own positioning line, verbatim — unfalsifiable, and reads as
       * fair. Optional because a platform feature is not marketed and has no
       * such line; inventing one would be worse than omitting it.
       */
      quote: z.string().optional(),
      summary: z.string(),
    }),
    /** The one line the quarterly re-verify pass checks: is this still true? */
    thesis: z.string(),
    description: z.string(),
    /** A "with" page rather than a "vs" page — reframes the whole header. */
    complement: z.boolean().default(false),
    verdict: z.object({
      chooseThem: z.array(z.string()).min(3),
      chooseUs: z.array(z.string()).min(3),
    }),
    us: z.string(),
    difference: z.object({ title: z.string(), body: z.array(z.string()).min(1) }),
    theirWins: z.array(claim).min(3),
    ourWins: z.array(claim).min(3),
    rows: z.array(row).min(6),
    /** Present only when running both is genuinely the right answer. */
    both: z.object({ title: z.string(), body: z.string() }).optional(),
    closing: z.string(),
    verifiedAt: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "use YYYY-MM-DD"),
    theirVersion: z.string(),
  })
  .refine(
    (entry) => entry.rows.filter((r) => r.us.value === "no" || r.us.value === "partial").length >= 3,
    { message: "the table needs at least 3 rows Hive does not win", path: ["rows"] },
  );

export type Comparison = z.infer<typeof comparisonSchema>;

const hubSchema = z.object({
  title: z.string(),
  description: z.string(),
  eyebrow: z.string(),
  intro: z.string(),
  argument: z.object({ title: z.string(), body: z.array(z.string()).min(1) }),
});

function parse<T extends z.ZodTypeAny>(name: string, schema: T, value: unknown): z.infer<T> {
  const result = schema.safeParse(value);
  if (!result.success) {
    throw new Error(`src/data/compare/${name}.json is invalid:\n${result.error.message}`);
  }
  return result.data;
}

export const hub = parse("hub", hubSchema, hubJson);

/** Hub order: the names people actually search for come first. */
export const comparisons: Comparison[] = [
  parse("herdr", comparisonSchema, herdrJson),
  parse("github-notifications", comparisonSchema, githubNotificationsJson),
  parse("claude-code", comparisonSchema, claudeCodeJson),
  parse("octobox", comparisonSchema, octoboxJson),
];
