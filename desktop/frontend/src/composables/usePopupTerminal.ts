import { ref, shallowRef, type Ref, type ShallowRef } from 'vue'
import {
  createPopupTerminalClient,
  getPopupTerminalEndpoint,
  type PopupTerminalClient,
  type PopupTerminalRequest,
} from '../lib/popupTerminalClient'
import { Available } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/popupterminalservice'

// The pop-up terminal's panel state, shared by whatever asks for one — the
// command palette, a keybinding, later a launcher — and the panel itself, which
// is mounted once at the app root. Hiding the panel leaves the shell running:
// the terminal is ephemeral in that it dies with the app, not in that it dies
// with a keystroke.

const visible = ref(false)
const checking = ref(true)
const available = ref(false)
const reason = ref('')
const client: ShallowRef<PopupTerminalClient | null> = shallowRef(null)

// What the next terminal opens against. It is captured when the panel is asked
// for rather than when the terminal is opened, because that is the moment the
// caller's context — the session on screen — is known.
const request = ref<PopupTerminalRequest>({})

let probe: Promise<void> | null = null

export function usePopupTerminal(): {
  visible: Ref<boolean>
  checking: Ref<boolean>
  available: Ref<boolean>
  reason: Ref<string>
  client: ShallowRef<PopupTerminalClient | null>
  request: Ref<PopupTerminalRequest>
  toggle: (next?: PopupTerminalRequest) => void
  show: (next?: PopupTerminalRequest) => void
  hide: () => void
  /** Settles once availability and the transport are known. */
  ready: () => Promise<void>
} {
  return { visible, checking, available, reason, client, request, toggle, show, hide, ready: ensureProbed }
}

function show(next: PopupTerminalRequest = {}): void {
  request.value = next
  visible.value = true
  void ensureProbed()
}

function hide(): void {
  visible.value = false
}

function toggle(next: PopupTerminalRequest = {}): void {
  if (visible.value) hide()
  else show(next)
}

// The availability answer and the transport are resolved once per run: neither
// can change while the app is running — there is no program to install — so a
// second probe would only re-ask a settled question.
function ensureProbed(): Promise<void> {
  probe ??= (async () => {
    try {
      const availability = await Available()
      available.value = availability.available
      reason.value = availability.reason
      if (availability.available) client.value = createPopupTerminalClient(await getPopupTerminalEndpoint())
    } catch (error) {
      available.value = false
      reason.value = error instanceof Error ? error.message : 'The pop-up terminal is unavailable.'
    } finally {
      checking.value = false
    }
  })()
  return probe
}

export function resetPopupTerminalForTests(): void {
  visible.value = false
  checking.value = true
  available.value = false
  reason.value = ''
  client.value = null
  request.value = {}
  probe = null
}
