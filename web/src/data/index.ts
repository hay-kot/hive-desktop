/**
 * Site content lives in the JSON files beside this module. Everything is parsed
 * through a schema at build time, so a malformed edit fails `astro build`
 * instead of silently rendering a broken page.
 */
import { z } from "astro/zod";

import betaJson from "./beta.json";
import featuresJson from "./features.json";
import heroJson from "./hero.json";
import onboardingJson from "./onboarding.json";
import pipelineJson from "./pipeline.json";
import previewJson from "./preview.json";
import pricingJson from "./pricing.json";
import qolJson from "./qol.json";
import siteJson from "./site.json";

/** Palette keys components map to CSS custom properties. */
const accent = z.enum(["amber", "green", "green2", "blue", "violet", "red", "muted"]);

/**
 * A nav/footer destination. `href: null` means "we do not have this page yet" —
 * such links are dropped at render time rather than pointing at a dead anchor.
 * Filling in the URL is the only edit needed to bring one back.
 */
const link = z.object({
  label: z.string(),
  href: z.string().nullable(),
  external: z.boolean().optional(),
  accent: z.boolean().optional(),
});

const siteSchema = z.object({
  brand: z.object({ name: z.string(), tagline: z.string() }),
  version: z.string(),
  betaBadge: z.string(),
  description: z.string(),
  nav: z.array(link),
  navCta: link,
  footerLinks: z.array(link),
  copyrightSuffix: z.string(),
});

const heroSchema = z.object({
  eyebrow: z.string(),
  titleLines: z.array(z.string()),
  subtitle: z.string(),
  primaryCta: link,
  secondaryCta: link,
  note: z.string(),
});

const qolSchema = z.object({
  eyebrow: z.string(),
  title: z.string(),
  intro: z.string(),
  items: z.array(z.object({ glyph: z.string(), title: z.string(), body: z.string() })),
});

const featuresSchema = z.array(
  z.object({
    icon: z.string(),
    accent,
    title: z.string(),
    body: z.string(),
    meta: z.string(),
  }),
);

const cardItem = z.object({
  icon: z.string(),
  title: z.string(),
  meta: z.string(),
  tag: z.string().optional(),
  tagAccent: accent.optional(),
  wide: z.boolean().optional(),
  dashed: z.boolean().optional(),
});

const pipelineSchema = z.object({
  eyebrow: z.string(),
  titleLines: z.array(z.string()),
  body: z.string(),
  diagram: z.object({
    sources: z.array(
      z.object({
        name: z.string(),
        slug: z.string(),
        icon: z.string(),
        accent: z.string(),
        stat: z.string(),
        /** Not implemented yet — drawn dashed, with no live throughput. */
        planned: z.boolean().optional(),
      }),
    ),
    filters: z.array(
      z.object({
        name: z.string(),
        slug: z.string(),
        stat: z.string(),
        /** Defaults to the filter glyph; set for other mid-graph node types. */
        icon: z.string().optional(),
      }),
    ),
    feeds: z.array(z.object({ name: z.string(), stat: z.string() })),
    /** Extra source → filter edges beyond the straight-across default. */
    crossLinks: z.array(z.object({ from: z.number(), to: z.number() })),
    /** filter index → feed index. `flow: true` animates a packet along it. */
    routes: z.array(
      z.object({ from: z.number(), to: z.number(), flow: z.boolean().optional() }),
    ),
  }),
  sourcesCard: z.object({
    label: z.string(),
    hint: z.string(),
    accent,
    items: z.array(cardItem),
  }),
  nodesCard: z.object({
    label: z.string(),
    hint: z.string(),
    accent,
    items: z.array(cardItem),
  }),
});

const onboardingSchema = z.object({
  eyebrow: z.string(),
  title: z.string(),
  body: z.string(),
  steps: z.array(z.object({ title: z.string(), body: z.string() })),
  editor: z.object({ path: z.string(), filename: z.string() }),
  pills: z.array(z.object({ icon: z.string(), accent, label: z.string() })),
  footnote: z.string(),
});

const betaSchema = z.object({
  status: z.string(),
  titleLines: z.array(z.string()),
  body: z.string(),
  perks: z.array(z.object({ icon: z.string(), accent, label: z.string() })),
  /** Copy for the request-access dialog the #beta links open. */
  dialog: z.object({ title: z.string(), body: z.string() }),
  form: z.object({
    label: z.string(),
    placeholder: z.string(),
    submit: z.string(),
    endpoint: z.string(),
  }),
  success: z.object({ title: z.string(), body: z.string() }),
  errors: z.record(z.string(), z.string()),
});

const pricingSchema = z.object({
  eyebrow: z.string(),
  title: z.string(),
  body: z.string(),
  tiers: z.array(
    z.object({
      name: z.string(),
      note: z.string(),
      price: z.string(),
      priceNote: z.string(),
      strikePrice: z.string().optional(),
      badge: z.string().optional(),
      accent,
      highlight: z.boolean().optional(),
      features: z.array(z.object({ label: z.string(), muted: z.boolean().optional() })),
      cta: z.object({
        label: z.string(),
        href: z.string().nullable(),
        variant: z.enum(["ghost"]).optional(),
      }),
      ctaNote: z.string(),
    }),
  ),
  explainer: z.array(
    z.object({ label: z.string(), accent: accent.optional(), body: z.string() }),
  ),
  /** Where buy buttons point until a real checkout exists. */
  checkoutFallbackHref: z.string(),
});

/** Item kinds the app colours distinctly (see --hv-kind-* in the app theme). */
const itemKind = z.enum(["issue", "pr"]);

const feedRow = z.object({
  label: z.string(),
  icon: z.string().optional(),
  count: z.string(),
  selected: z.boolean().optional(),
});

const previewSchema = z.object({
  titlebar: z.object({ command: z.string(), shortcut: z.string(), activity: z.string() }),
  spaces: z.array(z.object({ initials: z.string(), active: z.boolean().optional() })),
  sidebar: z.object({
    title: z.string(),
    sources: z.string(),
    feedsLabel: z.string(),
    /** A row is either a feed or a folder holding feeds — one level deep. */
    feeds: z.array(
      feedRow.extend({
        folder: z.boolean().optional(),
        children: z.array(feedRow).optional(),
      }),
    ),
    /** Feeds are the app's only primary destinations; Trash sits under them as
     * a de-emphasised utility surface. There is no aggregate inbox view. */
    trash: feedRow,
    footer: z.object({ title: z.string(), subtitle: z.string() }),
  }),
  search: z.object({ placeholder: z.string() }),
  tabs: z.object({ all: z.string(), unread: z.string(), unreadCount: z.string() }),
  view: z.string(),
  items: z.array(
    z.object({
      title: z.string(),
      age: z.string(),
      type: z.string(),
      accent: itemKind,
      context: z.string(),
      snippet: z.string(),
      selected: z.boolean().optional(),
    }),
  ),
  detail: z.object({
    type: z.string(),
    accent: itemKind,
    context: z.string(),
    open: z.string(),
    title: z.string(),
    byline: z.string(),
    actionsLabel: z.string(),
    actionsFor: z.string(),
    edit: z.string(),
    actions: z.array(
      z.object({
        icon: z.string(),
        accent,
        title: z.string(),
        meta: z.string(),
        run: z.string().optional(),
      }),
    ),
    footnote: z.string(),
  }),
});

/** Reports which file failed rather than dumping a bare Zod trace. */
function parse<T extends z.ZodTypeAny>(name: string, schema: T, value: unknown): z.infer<T> {
  const result = schema.safeParse(value);
  if (!result.success) {
    throw new Error(`src/data/${name}.json is invalid:\n${result.error.message}`);
  }
  return result.data;
}

export const site = parse("site", siteSchema, siteJson);
export const hero = parse("hero", heroSchema, heroJson);
export const qol = parse("qol", qolSchema, qolJson);
export const features = parse("features", featuresSchema, featuresJson);
export const pipeline = parse("pipeline", pipelineSchema, pipelineJson);
export const onboarding = parse("onboarding", onboardingSchema, onboardingJson);
export const beta = parse("beta", betaSchema, betaJson);
export const pricing = parse("pricing", pricingSchema, pricingJson);
export const preview = parse("preview", previewSchema, previewJson);

export type Link = z.infer<typeof link>;
export type Accent = z.infer<typeof accent>;

/** Drops links whose destination does not exist yet. */
export function resolved(links: Link[]): (Link & { href: string })[] {
  return links.filter((entry): entry is Link & { href: string } => entry.href !== null);
}
