// App-registry entry (D2) — never imports runtime.ts. role 'source' means
// "backend-run", so there is no worker-side code for this type.
import editor from './editor.vue'
import help from '@nodedocs/sources.grafana_alerts.md?raw'
import { accentToken, category, defaults, glyph, label, role, tint, type, validate } from './config'
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
  outputs: 1,
  validate,
  editor,
  help,
})
