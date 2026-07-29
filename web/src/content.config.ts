import { defineCollection, z } from "astro:content";
import { glob } from "astro/loaders";

import { DOC_GROUPS } from "./lib/docs";

const docs = defineCollection({
  loader: glob({ pattern: "**/[^_]*.md", base: "./src/content/docs" }),
  schema: z.object({
    title: z.string(),
    description: z.string(),
    group: z.enum(DOC_GROUPS),
    order: z.number().default(0),
    draft: z.boolean().default(false),
  }),
});

export const collections = { docs };
