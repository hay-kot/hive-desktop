// App-registry entry — webhook-source has no runtime.ts: role 'source' means
// "backend-run" (the local webhook listener ingests deliveries in Go), so
// there is no worker-side code for this type.
import editor from './editor.vue'
import help from './help.md?raw'
import { accentToken, category, defaults, freshConfig, glyph, label, role, tint, type, validate } from './config'
import { defineNodeType } from '../../nodeType'

export default defineNodeType({
  type,
  label,
  category,
  role,
  glyph,
  accentToken,
  tint,
  defaults,
  freshConfig,
  outputs: 1,
  validate,
  editor,
  help,
})
