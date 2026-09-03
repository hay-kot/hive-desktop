<script setup lang="ts">
// Create/edit editor for an agent workspace's manifest, in the app's
// DrawerSheet editor shell (the ActionEditor pattern). It writes the fields
// it shows — name, agent, autonomy, and the mcps and skills lists (plus the
// directory name at creation); hand-written comments in the YAML survive the
// write untouched. Both capability lists work the same way: a shared library
// on disk declares what exists (mcps.yaml, skills.yml) and the toggle is this
// workspace's own. The skills list names packages, not individual skills
// (ADR skill-packages-are-the-unit-a-workspace-enables). Deleting the workspace also lives here — the editor
// is the workspace's whole management surface, so its sidebar row needs no
// menu. Delete follows FolderEditModal.vue's shape: a quiet footer action
// that expands into an InlineConfirm over a dimmed, inert form.
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import IconChevronDown from '~icons/lucide/chevron-down'
import IconExternalLink from '~icons/lucide/external-link'
import IconFolderCog from '~icons/lucide/folder-cog'
import IconFolderOpen from '~icons/lucide/folder-open'
import IconMessageSquare from '~icons/lucide/message-square'
import IconPencil from '~icons/lucide/pencil'
import IconPlay from '~icons/lucide/play'
import IconPlus from '~icons/lucide/plus'
import IconTrash2 from '~icons/lucide/trash-2'
import IconTriangleAlert from '~icons/lucide/triangle-alert'
import IconX from '~icons/lucide/x'
import AppSelect, { type AppSelectOption } from './AppSelect.vue'
import AppSwitch from './AppSwitch.vue'
import BaseButton from './BaseButton.vue'
import DrawerSheet from './DrawerSheet.vue'
import InlineConfirm from './InlineConfirm.vue'
import { CodeField, SelectField, TextField, TextareaField, type SelectOption } from '../pipeline/fields'
import { useAgentSchedules } from '../composables/useAgentSchedules'
import { useAgentWorkspaces } from '../composables/useAgentWorkspaces'
import { timeLabel } from '../lib/activityPresentation'
import { relativeAge } from '../lib/age'
import {
  buildCron, dayAbbreviation, defaultShape, describe, parseCron, WEEK_ORDER, type ScheduleShape,
} from '../lib/scheduleShape'
import type {
  AgentSchedule, AgentScheduleRun, AgentWorkspace, ScheduleEdit, SkillPackageMember, WorkspaceEditRequest,
} from '../lib/agentWorkspacesClient'

const props = defineProps<{
  /** The workspace being edited, or null to create one. */
  workspace?: AgentWorkspace | null
  agents: string[]
  busy?: boolean
  error?: string | null
}>()
const emit = defineEmits<{
  close: []
  save: [request: WorkspaceEditRequest]
  delete: [dir: string]
  /** A chat one of this workspace's runs launched; the area resumes it like a sidebar click. */
  'open-chat': [session: number]
}>()

const {
  root, editor, mcpCatalogue, skillPackages, skillNames, skillPackagesProblem, autonomyFlags,
  reloadMCPCatalogue, importMCPServers, removeMCPServer,
  reloadSkillPackages, revealSkillPackages, revealSharedSkills,
  openWorkspaceInEditor, revealWorkspace,
} = useAgentWorkspaces()

const creating = computed(() => !props.workspace)

// The confirm strip names the folder it is about to delete, in full: the
// directory name alone reads as a label in the app, not as a path on disk.
const deletedPath = computed(() => (root.value ? `${root.value}/${props.workspace?.dir ?? ''}` : props.workspace?.dir ?? ''))

// A manifest the loader could not read leaves every list on this form empty,
// and Save reconciles the file to what the form holds, so it would rewrite
// mcps:, skills: and schedules: to nothing. The Go side refuses such an update
// (KindInvalid); the form refuses it first, and says what to fix instead.
const manifestProblem = computed(() => (creating.value ? '' : props.workspace?.problem ?? ''))

const dir = ref(props.workspace?.dir ?? '')
const name = ref(props.workspace?.name ?? '')
const agent = ref(props.workspace?.agent || props.agents[0] || '')
const autonomy = ref(props.workspace?.autonomy || 'ask')
const selectedMCPs = ref<string[]>([...(props.workspace?.mcps ?? [])])
const selectedSkills = ref<string[]>([...(props.workspace?.skills ?? [])])

const agentOptions = computed<AppSelectOption[]>(() => props.agents.map((a) => ({ value: a, label: a })))

// Every posture is laid out as a radio card rather than a dropdown, so the
// choice being made — especially full's dangerous bypass — is readable
// before it is selected. The flags line is the launch table's own projection
// for the chosen agent (ADR a-workspace-declares-its-own-authority): the UI shows what the posture actually
// runs, never a euphemism, and a posture the launch would refuse is disabled.
const AUTONOMY_META = [
  {
    value: 'ask',
    label: 'Ask',
    description: 'Every action needs your approval — the agent prompts before anything it is not sure of.',
    danger: false,
  },
  {
    value: 'auto',
    label: 'Auto',
    description: 'File edits are allowed without prompting; anything riskier still asks.',
    danger: false,
  },
  {
    value: 'full',
    label: 'Full — dangerously skip permissions',
    description: 'Bypasses the agent\'s permission prompts entirely. Unattended, it can take any action your user account can, including through every enabled MCP server.',
    danger: true,
  },
] as const

interface AutonomyOption {
  value: string
  label: string
  description: string
  danger: boolean
  flags: string
  unavailable: boolean
}

const autonomyOptions = computed<AutonomyOption[]>(() => {
  const known = autonomyFlags.value[agent.value]
  return AUTONOMY_META.map((meta) => {
    const flags = known?.[meta.value]
    return {
      ...meta,
      flags: flags?.length ? flags.join(' ') : '',
      // Only a loaded table can rule a posture out; with nothing loaded the
      // selector stays fully usable and simply shows no flag detail.
      unavailable: !!known && !flags,
    }
  })
})

function selectAutonomy(option: AutonomyOption): void {
  if (props.busy || option.unavailable) return
  autonomy.value = option.value
}

const confirming = ref(false)

// ── MCP rows ─────────────────────────────────────────────────────────────────
// The catalogue's rows plus any declared id the catalogue no longer resolves,
// so a hand-authored entry that went missing is visible and removable rather
// than silently kept.
interface MCPRow {
  id: string
  title: string
  command: string
  problem: string
  shipped: boolean
  stability: string
  shadows: string
  missing: boolean
}

const mcpRows = computed<MCPRow[]>(() => {
  const rows: MCPRow[] = mcpCatalogue.value.map((e) => ({
    id: e.id, title: e.title || e.id, command: e.command, problem: e.problem,
    shipped: e.shipped, stability: e.stability, shadows: e.shadows, missing: false,
  }))
  const known = new Set(rows.map((r) => r.id))
  for (const id of selectedMCPs.value) {
    if (!known.has(id)) rows.push({ id, title: id, command: '', problem: '', shipped: false, stability: '', shadows: '', missing: true })
  }
  return rows
})

function mcpEnabled(id: string): boolean {
  return selectedMCPs.value.includes(id)
}

function toggleMCP(id: string): void {
  selectedMCPs.value = mcpEnabled(id)
    ? selectedMCPs.value.filter((x) => x !== id)
    : [...selectedMCPs.value, id]
}

const mcpError = ref('')

async function removeServer(id: string): Promise<void> {
  mcpError.value = ''
  try {
    await removeMCPServer(id)
    selectedMCPs.value = selectedMCPs.value.filter((x) => x !== id)
  } catch (failure) {
    mcpError.value = failure instanceof Error ? failure.message : 'The server could not be removed.'
  }
}

// ── Skill package rows ───────────────────────────────────────────────────────
// A workspace enables packages, not skills: skills.yml defines each package as
// glob patterns, and the rows show what those patterns select right now. An
// enabled name skills.yml no longer defines still rows, so it can be switched
// off rather than silently selecting nothing.
interface SkillPackageRow {
  name: string
  title: string
  description: string
  members: SkillPackageMember[]
  missing: boolean
  /** Why the name does not resolve, when it does not. */
  warning: string
}

const skillRows = computed<SkillPackageRow[]>(() => {
  const rows: SkillPackageRow[] = skillPackages.value.map((pkg) => ({
    name: pkg.name, title: pkg.title || pkg.name, description: pkg.description,
    members: pkg.members, missing: false, warning: '',
  }))
  const known = new Set(rows.map((r) => r.name))
  const bySlug = new Map(skillNames.value.map((skill) => [skill.slug, skill]))
  for (const name of selectedSkills.value) {
    if (known.has(name)) continue
    rows.push({ name, title: name, description: '', members: [], missing: true, warning: missingSkillWarning(name, bySlug.get(name)?.selectedBy) })
  }
  return rows
})

// A name skills.yml does not define is a package that never existed *or* a
// skill slug from a manifest written before packages were the enablement unit
// (#307). Only the second has a fix on screen, and saying "not defined in
// skills.yml" for both hides it.
function missingSkillWarning(name: string, selectedBy: string[] | undefined): string {
  if (!selectedBy) return 'not defined in skills.yml — an enabled package without a definition brings nothing'
  if (!selectedBy.length) return 'a skill, not a package — no package selects it yet, so define one in skills.yml'
  const packages = selectedBy.map((pkg) => `"${pkg}"`).join(' or ')
  return `a skill, not a package — the ${packages} package selects it, so enable that and switch this off`
}

function skillEnabled(name: string): boolean {
  return selectedSkills.value.includes(name)
}

function toggleSkill(name: string): void {
  selectedSkills.value = skillEnabled(name)
    ? selectedSkills.value.filter((x) => x !== name)
    : [...selectedSkills.value, name]
}

// A package's members are worth seeing before enabling it — it is the whole
// authority the package grants — but not worth the height by default, so the
// list expands on demand.
const expandedSkillPackages = ref<string[]>([])

function skillPackageExpanded(name: string): boolean {
  return expandedSkillPackages.value.includes(name)
}

function toggleSkillPackageExpanded(name: string): void {
  expandedSkillPackages.value = skillPackageExpanded(name)
    ? expandedSkillPackages.value.filter((x) => x !== name)
    : [...expandedSkillPackages.value, name]
}

const skillError = ref('')

async function openSkillPackages(): Promise<void> {
  skillError.value = ''
  try {
    await revealSkillPackages()
  } catch (failure) {
    skillError.value = failure instanceof Error ? failure.message : 'skills.yml could not be opened.'
  }
}

async function openSharedSkills(): Promise<void> {
  skillError.value = ''
  try {
    await revealSharedSkills()
  } catch (failure) {
    skillError.value = failure instanceof Error ? failure.message : 'The skills folder could not be opened.'
  }
}

// ── Schedules ────────────────────────────────────────────────────────────────
// A schedule is a manifest key like the lists above, so it is edited here and
// written by the same Save: the request carries the whole list and the Go side
// reconciles `schedules:` to it. Cron is still what the manifest stores, but
// the form never asks for one: a card edits a ScheduleShape and compiles it
// (lib/scheduleShape.ts), with the cron box reserved for expressions no shape
// can state. Only Run now and the run history reach the network from here;
// everything else is form state until Save.

/** Long enough that a keystroke does not cost a round trip, short enough to feel live. */
const PREVIEW_DEBOUNCE_MS = 300
/** How many of the preview's occurrences a card shows. */
const PREVIEW_SHOWN = 3
const DAY_MS = 24 * 60 * 60 * 1000
/** Under a minute out, a countdown reads as noise; the card says it is up instead. */
const IMMINENT_MS = 60 * 1000
const SCHEDULE_ID = /^[a-z0-9][a-z0-9-]*$/
const QUARTER_MINUTES = [0, 15, 30, 45]

interface ScheduleCard {
  /** Identity for v-for and the per-card preview timer, stable while the id is still being typed. */
  key: string
  /** Fixed once saved: the run history and the scheduler's cursor are keyed by it. */
  id: string
  name: string
  shape: ScheduleShape
  prompt: string
  disabled: boolean
  onMissed: string
  /** False until a Save writes it, which is what Run now and the history need. */
  saved: boolean
  nextRunAt: number | null
  lastRun: AgentScheduleRun | null
  expanded: boolean
  removing: boolean
  /** Whether the minute picker is showing its free number input. */
  minuteFree: boolean
  cronError: string
  promptError: string
  next: number[]
  running: boolean
  actionError: string
  historyOpen: boolean
  historyLoaded: boolean
  historyError: string
  history: AgentScheduleRun[]
}

const { list: listSchedules, runs: listScheduleRuns, runNow, preview } = useAgentSchedules()

let cardSeq = 0

function newCard(overrides: Partial<ScheduleCard> = {}): ScheduleCard {
  return {
    key: `card-${++cardSeq}`, id: '', name: '', shape: defaultShape(), prompt: '',
    disabled: false, onMissed: 'run', saved: false, nextRunAt: null, lastRun: null,
    expanded: false, removing: false, minuteFree: false, cronError: '', promptError: '',
    next: [], running: false, actionError: '', historyOpen: false, historyLoaded: false,
    historyError: '', history: [], ...overrides,
  }
}

function cardFrom(schedule: AgentSchedule): ScheduleCard {
  const shape = parseCron(schedule.cron)
  return newCard({
    id: schedule.id,
    // The manifest's own name, blank included: a hand-authored entry that
    // never named itself must not come back with `name: <id>` written into it.
    name: schedule.name,
    shape,
    prompt: schedule.prompt,
    disabled: schedule.disabled,
    onMissed: schedule.onMissed || 'run',
    saved: true,
    nextRunAt: schedule.nextRunAt,
    lastRun: schedule.lastRun,
    minuteFree: shape.kind === 'hourly' && !QUARTER_MINUTES.includes(shape.minute),
  })
}

const scheduleCards = ref<ScheduleCard[]>((props.workspace?.schedules ?? []).map(cardFrom))

const REPEAT_OPTIONS: SelectOption[] = [
  { value: 'hourly', label: 'Hourly' },
  { value: 'daily', label: 'Daily' },
  { value: 'weekly', label: 'Weekly' },
  { value: 'monthly', label: 'Monthly' },
  { value: 'custom', label: 'Custom' },
]

const MINUTE_OPTIONS: SelectOption[] = [
  ...QUARTER_MINUTES.map((minute) => ({ value: String(minute), label: `:${pad2(minute)}` })),
  { value: 'other', label: 'Other minute…' },
]

const MONTH_DAY_OPTIONS: SelectOption[] = Array.from({ length: 31 }, (_, index) => ({
  value: String(index + 1),
  label: String(index + 1),
}))

const ON_MISSED_OPTIONS: SelectOption[] = [
  { value: 'run', label: 'Run once when the app is back' },
  { value: 'skip', label: 'Skip' },
]

const PROMPT_VARIABLES = [
  '{{ .Now }}',
  '{{ .ScheduledFor }}',
  '{{ .LastRun }}',
  '{{ .Reason }}',
  '{{ .Schedule.Name }}',
  '{{ .Workspace.Name }}',
  '{{ date "2006-01-02" .Now }}',
]
const promptHint = `Go template. Available: ${PROMPT_VARIABLES.join(', ')}`

function pad2(value: number): string {
  return String(value).padStart(2, '0')
}

// A new schedule's id follows its name so the manifest reads as prose; a saved
// one is fixed, because the id is what the run history and the scheduler's
// cursor are keyed by. That is also why a name is required to add one and
// optional to keep one: the id is the only thing that has to exist.
function scheduleId(card: ScheduleCard): string {
  if (card.saved) return card.id
  return card.name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
}

function scheduleLabel(card: ScheduleCard): string {
  return card.name.trim() || card.id || 'Untitled schedule'
}

const duplicateScheduleIds = computed(() => {
  const seen = new Set<string>()
  const duplicates = new Set<string>()
  for (const card of scheduleCards.value) {
    const id = scheduleId(card)
    if (!id) continue
    if (seen.has(id)) duplicates.add(id)
    else seen.add(id)
  }
  return duplicates
})

/** What stops this card being saved, in the order a user would fix it. */
function scheduleProblem(card: ScheduleCard): string {
  const id = scheduleId(card)
  if (!card.saved) {
    if (!card.name.trim()) return 'A name is required.'
    if (!SCHEDULE_ID.test(id)) return 'The name needs a letter or a digit to build an id from.'
  }
  if (duplicateScheduleIds.value.has(id)) return `Another schedule already uses the id "${id}".`
  if (card.shape.kind === 'weekly' && !card.shape.days.length) return 'Pick at least one day.'
  if (card.shape.kind === 'custom' && !card.shape.cron.trim()) return 'A cron expression is required.'
  if (!card.prompt.trim()) return 'A prompt is required.'
  return ''
}

function scheduleBlocked(card: ScheduleCard): boolean {
  return !!scheduleProblem(card) || !!card.cronError || !!card.promptError
}

const schedulesValid = computed(() => scheduleCards.value.every((card) => !scheduleBlocked(card)))

const valid = computed(() =>
  !!name.value.trim() && !!agent.value && (!creating.value || !!dir.value.trim())
  && schedulesValid.value && !manifestProblem.value)

function addSchedule(): void {
  scheduleCards.value = [...scheduleCards.value, newCard({ expanded: true })]
}

function toggleScheduleExpanded(card: ScheduleCard): void {
  card.expanded = !card.expanded
  if (card.expanded) queuePreview(card)
}

function removeSchedule(card: ScheduleCard): void {
  scheduleCards.value = scheduleCards.value.filter((row) => row.key !== card.key)
}

// ── The card's shape controls ───────────────────────────────────────────────
function shapeClock(shape: ScheduleShape): { hour: number; minute: number } {
  if (shape.kind === 'custom') return { hour: 9, minute: 0 }
  if (shape.kind === 'hourly') return { hour: 9, minute: shape.minute }
  return { hour: shape.hour, minute: shape.minute }
}

function setRepeat(card: ScheduleCard, kind: string): void {
  if (kind === card.shape.kind) return
  const { hour, minute } = shapeClock(card.shape)
  switch (kind) {
    case 'hourly':
      card.shape = { kind: 'hourly', minute }
      card.minuteFree = !QUARTER_MINUTES.includes(minute)
      break
    case 'daily': card.shape = { kind: 'daily', hour, minute }; break
    case 'weekly': card.shape = { kind: 'weekly', days: [5], hour, minute }; break
    case 'monthly': card.shape = { kind: 'monthly', day: 1, hour, minute }; break
    // Switching to Custom carries the compiled expression over, so the box
    // opens on what the picker was already saying rather than empty.
    default: card.shape = { kind: 'custom', cron: buildCron(card.shape) }
  }
  queuePreview(card)
}

function dayPicked(card: ScheduleCard, day: number): boolean {
  return card.shape.kind === 'weekly' && card.shape.days.includes(day)
}

function toggleDay(card: ScheduleCard, day: number): void {
  if (card.shape.kind !== 'weekly') return
  const days = dayPicked(card, day)
    ? card.shape.days.filter((picked) => picked !== day)
    : [...card.shape.days, day].sort((a, b) => a - b)
  card.shape = { ...card.shape, days }
  // No days compiles to a four-field expression the Go side rejects, and the
  // card already says a day is missing; asking would only trade that for a
  // cron error about the wrong thing.
  if (days.length) queuePreview(card)
}

function timeValue(shape: ScheduleShape): string {
  if (shape.kind === 'custom' || shape.kind === 'hourly') return ''
  return `${pad2(shape.hour)}:${pad2(shape.minute)}`
}

function setTime(card: ScheduleCard, value: string): void {
  const match = /^(\d{1,2}):(\d{2})/.exec(value)
  if (!match || card.shape.kind === 'custom' || card.shape.kind === 'hourly') return
  card.shape = { ...card.shape, hour: Number(match[1]), minute: Number(match[2]) }
  queuePreview(card)
}

function minuteSelection(card: ScheduleCard): string {
  if (card.shape.kind !== 'hourly') return '0'
  return card.minuteFree ? 'other' : String(card.shape.minute)
}

function setMinuteSelection(card: ScheduleCard, value: string): void {
  if (value === 'other') {
    card.minuteFree = true
    return
  }
  card.minuteFree = false
  setMinute(card, Number(value))
}

function setMinute(card: ScheduleCard, minute: number): void {
  if (card.shape.kind !== 'hourly' || !Number.isFinite(minute)) return
  card.shape = { kind: 'hourly', minute: Math.min(59, Math.max(0, Math.trunc(minute))) }
  queuePreview(card)
}

function onTimeInput(card: ScheduleCard, event: Event): void {
  setTime(card, (event.target as HTMLInputElement).value)
}

function onMinuteInput(card: ScheduleCard, event: Event): void {
  setMinute(card, Number((event.target as HTMLInputElement).value))
}

function setMonthDay(card: ScheduleCard, value: string): void {
  if (card.shape.kind !== 'monthly') return
  card.shape = { ...card.shape, day: Number(value) }
  queuePreview(card)
}

function setCron(card: ScheduleCard, cron: string): void {
  card.shape = { kind: 'custom', cron }
  queuePreview(card)
}

function setPrompt(card: ScheduleCard, prompt: string): void {
  card.prompt = prompt
  queuePreview(card)
}

// ── Preview ─────────────────────────────────────────────────────────────────
// The Go side is the only thing that knows whether a cron and a template are
// valid, so the card debounces the question and renders the answer beside the
// field that raised it. One timer and one token per card: two cards open at
// once must not answer each other's question.
const previewTimers = new Map<string, ReturnType<typeof setTimeout>>()
const previewTokens = new Map<string, number>()

function queuePreview(card: ScheduleCard): void {
  // A workspace being created has nothing on disk to preview against; the
  // structured shapes compile to valid cron without asking.
  if (!props.workspace) return
  clearTimeout(previewTimers.get(card.key))
  previewTimers.set(card.key, setTimeout(() => { void runPreview(card) }, PREVIEW_DEBOUNCE_MS))
}

async function runPreview(card: ScheduleCard): Promise<void> {
  const token = (previewTokens.get(card.key) ?? 0) + 1
  previewTokens.set(card.key, token)
  try {
    const result = await preview({
      workspace: props.workspace!.dir,
      cron: buildCron(card.shape),
      prompt: card.prompt,
    })
    if (previewTokens.get(card.key) !== token) return
    card.next = result.next.slice(0, PREVIEW_SHOWN)
    card.cronError = result.cronError
    card.promptError = result.promptError
  } catch {
    // A preview that could not be asked for is not itself a reason to block
    // the save: the Go side validates the manifest again on the way in. The
    // errors go with the occurrences, since they answered an older cron and
    // prompt and would otherwise disable Save for good.
    if (previewTokens.get(card.key) !== token) return
    card.next = []
    card.cronError = ''
    card.promptError = ''
  }
}

onBeforeUnmount(() => {
  for (const timer of previewTimers.values()) clearTimeout(timer)
})

// ── Run now, and what a card says about its runs ────────────────────────────
async function runScheduleNow(card: ScheduleCard): Promise<void> {
  if (!props.workspace || !card.saved || card.running) return
  card.running = true
  card.actionError = ''
  try {
    await runNow(props.workspace.dir, card.id)
    await refreshRunState()
    if (card.historyLoaded) await loadHistory(card, true)
  } catch (failure) {
    card.actionError = failure instanceof Error ? failure.message : 'The schedule could not be run.'
  } finally {
    card.running = false
  }
}

// The run state is the Go side's, not the form's: a manual run moves nextRunAt
// and lastRun, and nothing in the form can compute either.
async function refreshRunState(): Promise<void> {
  if (!props.workspace) return
  const rows = await listSchedules(props.workspace.dir)
  const byId = new Map(rows.map((row) => [row.id, row]))
  for (const card of scheduleCards.value) {
    const row = card.saved ? byId.get(card.id) : undefined
    if (!row) continue
    card.nextRunAt = row.nextRunAt
    card.lastRun = row.lastRun
  }
}

async function toggleHistory(card: ScheduleCard): Promise<void> {
  card.historyOpen = !card.historyOpen
  if (card.historyOpen && !card.historyLoaded) await loadHistory(card)
}

async function loadHistory(card: ScheduleCard, refresh = false): Promise<void> {
  if (!props.workspace || !card.saved) return
  if (card.historyLoaded && !refresh) return
  card.historyError = ''
  try {
    card.history = await listScheduleRuns(props.workspace.dir, card.id)
    card.historyLoaded = true
  } catch (failure) {
    card.historyError = failure instanceof Error ? failure.message : 'The run history could not be read.'
  }
}

function nextRunLabel(card: ScheduleCard): string {
  if (card.disabled) return 'paused'
  if (!card.saved) return 'not saved yet'
  if (card.nextRunAt === null) return 'not scheduled'
  const now = Date.now()
  const delta = card.nextRunAt - now
  if (delta <= IMMINENT_MS) return 'due now'
  // relativeAge measures backwards from its second argument, so the two are
  // swapped here to get the same terse form ("2h") for a time still to come.
  if (delta < DAY_MS) return `in ${relativeAge(now, card.nextRunAt)}`
  return occurrence(card.nextRunAt)
}

function occurrence(at: number): string {
  return new Date(at).toLocaleString([], {
    weekday: 'short', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false,
  })
}

const REASON_LABELS: Record<string, string> = { due: 'due', catch_up: 'catch-up', manual: 'manual' }

function reasonLabel(run: AgentScheduleRun): string {
  return REASON_LABELS[run.reason] ?? run.reason
}

// JobsPopover's pill vocabulary: a launch reads as success, a failure as an
// error, and everything else stays neutral chrome.
function statusClasses(status: string): string {
  if (status === 'failed') return 'border-severity-error-border bg-severity-error-tint text-severity-error'
  if (status === 'launched') return 'border-severity-success-border bg-severity-success-tint text-severity-success'
  return 'border-border bg-chip text-text-2'
}

// A history spanning days needs the day; a run from today does not.
function runStamp(at: number): string {
  const when = new Date(at)
  if (when.toDateString() === new Date().toDateString()) return timeLabel(at)
  return `${when.toLocaleDateString([], { month: 'short', day: 'numeric' })} ${timeLabel(at)}`
}

function scheduleEdits(): ScheduleEdit[] {
  return scheduleCards.value.map((card) => ({
    id: scheduleId(card),
    name: card.name.trim(),
    cron: buildCron(card.shape),
    prompt: card.prompt,
    disabled: card.disabled,
    onMissed: card.onMissed,
  }))
}

// ── Paste-in JSON import ─────────────────────────────────────────────────────
const importOpen = ref(false)
const importText = ref('')
const importBusy = ref(false)
const importPlaceholder = '{"mcpServers": {"my-server": {"command": "npx", "args": ["-y", "…"]}}}'

function formatImportJSON(): void {
  mcpError.value = ''
  try {
    importText.value = JSON.stringify(JSON.parse(importText.value), null, 2)
  } catch (failure) {
    mcpError.value = failure instanceof Error ? `Not valid JSON: ${failure.message}` : 'Not valid JSON.'
  }
}

async function submitImport(): Promise<void> {
  if (!importText.value.trim() || importBusy.value) return
  importBusy.value = true
  mcpError.value = ''
  try {
    const added = await importMCPServers(importText.value)
    for (const id of added) {
      if (!mcpEnabled(id)) selectedMCPs.value = [...selectedMCPs.value, id]
    }
    importText.value = ''
    importOpen.value = false
  } catch (failure) {
    mcpError.value = failure instanceof Error ? failure.message : 'The pasted configuration could not be imported.'
  } finally {
    importBusy.value = false
  }
}

// ── Open the directory outside the app ───────────────────────────────────────
const actionError = ref('')

async function openInEditor(): Promise<void> {
  actionError.value = ''
  try {
    await openWorkspaceInEditor(props.workspace!.dir)
  } catch (failure) {
    actionError.value = failure instanceof Error ? failure.message : 'The editor could not be opened.'
  }
}

async function reveal(): Promise<void> {
  actionError.value = ''
  try {
    await revealWorkspace(props.workspace!.dir)
  } catch (failure) {
    actionError.value = failure instanceof Error ? failure.message : 'The directory could not be opened.'
  }
}

function submit(): void {
  if (props.busy || confirming.value || !valid.value) return
  emit('save', {
    dir: creating.value ? dir.value.trim() : props.workspace!.dir,
    name: name.value.trim(),
    agent: agent.value,
    autonomy: autonomy.value,
    mcps: selectedMCPs.value,
    skills: selectedSkills.value,
    schedules: scheduleEdits(),
  })
}

function cancel(): void {
  if (!props.busy && !confirming.value) emit('close')
}

const nameInput = ref<HTMLInputElement | null>(null)
const dirInput = ref<HTMLInputElement | null>(null)
onMounted(async () => {
  void reloadMCPCatalogue()
  void reloadSkillPackages()
  await nextTick()
  ;(creating.value ? dirInput.value : nameInput.value)?.focus()
})
</script>

<template>
  <DrawerSheet
    :ariaLabel="creating ? 'New workspace' : 'Edit workspace'"
    testid="agent-workspace-editor"
    :default-size="460"
    :close-on-escape="!confirming"
    :close-on-backdrop="!confirming"
    @close="cancel"
  >
    <template #header>
      <div class="flex items-center gap-3">
        <span class="flex size-[38px] items-center justify-center rounded-[10px] bg-accent text-accent-contrast"><IconFolderCog class="size-[18px]" /></span>
        <div class="min-w-0 flex-1">
          <div class="text-[15px] font-semibold tracking-[-.01em]">{{ creating ? 'New workspace' : 'Edit workspace' }}</div>
          <div class="truncate font-mono text-[12px] text-text-3">{{ creating ? 'A directory an agent works in' : workspace!.dir }}</div>
        </div>
        <button class="text-text-3 hover:text-text disabled:opacity-50" aria-label="Close" :disabled="busy || confirming" @click="cancel"><IconX class="size-4" /></button>
      </div>
    </template>

    <!-- While a delete is pending the form recedes: dimmed and inert, so the
         two states can't be misread for each other (FolderEditModal's rule). -->
    <div class="flex flex-col gap-4 transition-opacity" :class="{ 'pointer-events-none opacity-45': confirming }">
      <!-- Above the open/reveal actions on purpose: the fix is in the file,
           and those two buttons are what reach it. -->
      <div
        v-if="manifestProblem"
        class="flex flex-col gap-1 rounded border border-severity-error bg-severity-error-tint px-3 py-2 text-xs leading-relaxed text-severity-error"
        data-testid="agent-workspace-editor-problem"
      >
        <p>{{ manifestProblem }}</p>
        <p>Fix agent-workspace.yaml before saving from here. This form could not read the file, so saving would rewrite its lists as empty. The buttons below open it.</p>
      </div>

      <div v-if="!creating" class="flex flex-col gap-1.5">
        <div class="flex items-center gap-2">
          <button
            v-if="editor.command"
            type="button"
            class="flex cursor-pointer items-center gap-1.5 rounded-[7px] border border-card px-2.5 py-1.5 text-[12px] font-medium text-text-2 hover:border-strong hover:text-text disabled:opacity-50"
            :disabled="busy"
            data-testid="agent-workspace-editor-open-editor"
            @click="openInEditor"
          ><IconExternalLink class="size-3.5" />Open in {{ editor.title }}</button>
          <button
            type="button"
            class="flex cursor-pointer items-center gap-1.5 rounded-[7px] border border-card px-2.5 py-1.5 text-[12px] font-medium text-text-2 hover:border-strong hover:text-text disabled:opacity-50"
            :disabled="busy"
            data-testid="agent-workspace-editor-reveal"
            @click="reveal"
          ><IconFolderOpen class="size-3.5" />Show in Finder</button>
        </div>
        <span v-if="!editor.command" class="text-xs text-text-4">Pick a default editor in Settings › General to open this directory in it.</span>
        <p v-if="actionError" class="text-xs text-severity-error" data-testid="agent-workspace-editor-action-error">{{ actionError }}</p>
      </div>

      <div v-if="creating" class="flex flex-col gap-1.5">
        <label for="agent-workspace-dir" class="text-xs text-text-3">Directory name</label>
        <input
          id="agent-workspace-dir"
          ref="dirInput"
          v-model="dir"
          type="text"
          placeholder="my-project"
          autocapitalize="off"
          autocorrect="off"
          spellcheck="false"
          :disabled="busy"
          class="w-full rounded-lg border border-strong bg-raised px-3 py-2.5 font-mono text-[13px] text-text outline-none focus:border-accent"
          data-testid="agent-workspace-editor-dir"
          @keydown.enter="submit"
        >
        <span class="text-xs text-text-4">A new directory under the workspace root, seeded with an AGENTS.md to shape.</span>
      </div>

      <div class="flex flex-col gap-1.5">
        <label for="agent-workspace-name" class="text-xs text-text-3">Name</label>
        <input
          id="agent-workspace-name"
          ref="nameInput"
          v-model="name"
          type="text"
          :disabled="busy"
          class="w-full rounded-lg border border-strong bg-raised px-3 py-2.5 text-[13.5px] text-text outline-none focus:border-accent"
          data-testid="agent-workspace-editor-name"
          @keydown.enter="submit"
        >
      </div>

      <div class="flex flex-col gap-1.5">
        <span class="text-xs text-text-3">Agent</span>
        <AppSelect
          v-model="agent"
          :options="agentOptions"
          placeholder="No agents configured"
          aria-label="Agent"
          testid="agent-workspace-editor-agent"
          :disabled="busy || !agents.length"
        />
      </div>

      <div class="flex flex-col gap-1.5">
        <span class="text-xs text-text-3">Autonomy</span>
        <div
          role="radiogroup"
          aria-label="Autonomy"
          class="flex flex-col divide-y divide-row rounded-lg border border-strong bg-raised"
          data-testid="agent-workspace-editor-autonomy"
        >
          <button
            v-for="option in autonomyOptions"
            :key="option.value"
            type="button"
            role="radio"
            :aria-checked="autonomy === option.value"
            :disabled="busy || option.unavailable"
            class="flex items-start gap-2.5 px-3 py-2.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-50 first:rounded-t-lg last:rounded-b-lg"
            :class="[
              busy || option.unavailable ? '' : 'cursor-pointer',
              autonomy === option.value && option.danger ? 'bg-severity-warning-tint' : '',
            ]"
            :data-testid="`agent-workspace-editor-autonomy-${option.value}`"
            @click="selectAutonomy(option)"
          >
            <span
              class="mt-0.5 flex size-3.5 shrink-0 items-center justify-center rounded-full border"
              :class="autonomy === option.value ? 'border-accent' : 'border-strong'"
            >
              <span v-if="autonomy === option.value" class="size-1.5 rounded-full bg-accent" />
            </span>
            <span class="min-w-0 flex-1">
              <span class="flex items-center gap-1.5">
                <IconTriangleAlert v-if="option.danger" class="size-3.5 shrink-0 text-severity-warning" aria-hidden="true" />
                <span class="text-[13px]" :class="option.danger ? 'text-severity-warning' : 'text-text'">{{ option.label }}</span>
              </span>
              <span class="mt-0.5 block text-[11.5px] leading-relaxed text-text-3">{{ option.description }}</span>
              <span
                v-if="option.flags"
                class="mt-0.5 block truncate font-mono text-[11px]"
                :class="option.danger ? 'text-severity-warning' : 'text-text-4'"
                :title="option.flags"
              >{{ agent }} {{ option.flags }}</span>
              <span v-if="option.unavailable" class="mt-0.5 block text-[11px] text-text-4">not available for this agent</span>
            </span>
          </button>
        </div>
      </div>

      <div class="flex flex-col gap-1.5" data-testid="agent-workspace-editor-mcps">
        <span class="text-xs text-text-3">MCP servers</span>
        <div v-if="mcpRows.length" class="flex flex-col divide-y divide-row rounded-lg border border-strong bg-raised">
          <div v-for="row in mcpRows" :key="row.id" class="flex items-start gap-2.5 px-3 py-2.5">
            <AppSwitch
              size="sm"
              class="mt-0.5"
              :model-value="mcpEnabled(row.id)"
              :aria-label="`Enable ${row.title}`"
              :disabled="busy"
              :testid="`agent-workspace-editor-mcp-${row.id}`"
              @update:model-value="toggleMCP(row.id)"
            />
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-1.5">
                <span class="truncate text-[13px] text-text">{{ row.title }}</span>
                <span
                  class="shrink-0 rounded-full border border-card px-1.5 py-px text-[10px] text-text-4"
                >{{ row.shipped ? row.stability : 'custom' }}</span>
                <span v-if="row.shadows" class="shrink-0 text-[10px] text-severity-warning">replaces shipped</span>
              </div>
              <div v-if="row.command" class="truncate font-mono text-[11px] text-text-4" :title="row.command">{{ row.command }}</div>
              <div v-if="row.problem" class="text-[11px] text-severity-warning">{{ row.problem }}</div>
              <div v-if="row.missing" class="text-[11px] text-severity-warning">not in the catalogue — enabled ids without an entry are skipped at launch</div>
            </div>
            <button
              v-if="!row.shipped && !row.missing"
              type="button"
              class="mt-0.5 shrink-0 cursor-pointer text-text-4 hover:text-severity-error disabled:opacity-50"
              :title="`Remove ${row.title} from mcps.yaml (every workspace loses it)`"
              :aria-label="`Remove ${row.title}`"
              :disabled="busy"
              :data-testid="`agent-workspace-editor-mcp-remove-${row.id}`"
              @click="removeServer(row.id)"
            ><IconTrash2 class="size-3.5" /></button>
          </div>
        </div>

        <div v-if="importOpen" class="flex flex-col gap-1.5">
          <CodeField
            v-model="importText"
            :rows="8"
            :placeholder="importPlaceholder"
            testid="agent-workspace-editor-mcp-import-text"
          />
          <div class="flex items-center gap-2">
            <BaseButton size="sm" :busy="importBusy" :disabled="!importText.trim()" data-testid="agent-workspace-editor-mcp-import-submit" @click="submitImport">Add servers</BaseButton>
            <BaseButton
              variant="secondary"
              size="sm"
              :disabled="importBusy || !importText.trim()"
              data-testid="agent-workspace-editor-mcp-import-format"
              @click="formatImportJSON"
            >Format JSON</BaseButton>
            <BaseButton variant="secondary" size="sm" :disabled="importBusy" @click="importOpen = false">Cancel</BaseButton>
          </div>
          <span class="text-xs text-text-4">Pasted servers land in mcps.yaml — the library every workspace picks from — and switch on here.</span>
        </div>
        <button
          v-else
          type="button"
          class="self-start cursor-pointer text-[12px] text-accent hover:underline"
          :disabled="busy"
          data-testid="agent-workspace-editor-mcp-import"
          @click="importOpen = true"
        >Add servers from JSON…</button>
        <p v-if="mcpError" class="text-xs text-severity-error" data-testid="agent-workspace-editor-mcp-error">{{ mcpError }}</p>
      </div>

      <div class="flex flex-col gap-1.5" data-testid="agent-workspace-editor-skills">
        <span class="text-xs text-text-3">Skill packages</span>
        <div v-if="skillRows.length" class="flex flex-col divide-y divide-row rounded-lg border border-strong bg-raised">
          <div v-for="row in skillRows" :key="row.name" class="flex flex-col">
            <div class="flex items-start gap-2.5 px-3 py-2.5">
              <AppSwitch
                size="sm"
                class="mt-0.5"
                :model-value="skillEnabled(row.name)"
                :aria-label="`Enable ${row.title}`"
                :disabled="busy"
                :testid="`agent-workspace-editor-skill-${row.name}`"
                @update:model-value="toggleSkill(row.name)"
              />
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-1.5">
                  <span class="truncate text-[13px] text-text">{{ row.title }}</span>
                  <button
                    v-if="!row.missing"
                    type="button"
                    class="flex shrink-0 cursor-pointer items-center gap-1 text-[11px] text-text-4 hover:text-text-3"
                    :aria-expanded="skillPackageExpanded(row.name)"
                    :data-testid="`agent-workspace-editor-skill-members-${row.name}`"
                    @click="toggleSkillPackageExpanded(row.name)"
                  >
                    {{ row.members.length }} {{ row.members.length === 1 ? 'skill' : 'skills' }}
                    <IconChevronDown class="size-3 transition-transform" :class="{ '-rotate-90': !skillPackageExpanded(row.name) }" />
                  </button>
                </div>
                <div v-if="row.description" class="text-[11.5px] leading-relaxed text-text-3">{{ row.description }}</div>
                <div v-if="!row.missing && !row.members.length" class="text-[11px] text-severity-warning">matches no skill — check its patterns in skills.yml</div>
                <div v-if="row.warning" class="text-[11px] text-severity-warning">{{ row.warning }}</div>
              </div>
            </div>
            <ul v-if="skillPackageExpanded(row.name) && row.members.length" class="flex flex-col gap-1 border-t border-row px-3 py-2 pl-9">
              <li v-for="member in row.members" :key="member.slug" class="flex items-center gap-1.5">
                <span class="truncate font-mono text-[11.5px] text-text-3">{{ member.slug }}</span>
                <span class="shrink-0 rounded-full border border-card px-1.5 py-px text-[10px] text-text-4">{{ member.shipped ? 'shipped' : 'custom' }}</span>
              </li>
            </ul>
          </div>
        </div>
        <span v-else class="text-xs text-text-4">No packages are defined yet.</span>
        <div class="flex items-center gap-3">
          <button
            type="button"
            class="cursor-pointer text-[12px] text-accent hover:underline"
            :disabled="busy"
            data-testid="agent-workspace-editor-skills-packages"
            @click="openSkillPackages"
          >Edit skills.yml…</button>
          <button
            type="button"
            class="cursor-pointer text-[12px] text-accent hover:underline"
            :disabled="busy"
            data-testid="agent-workspace-editor-skills-shared"
            @click="openSharedSkills"
          >Open the skills folder…</button>
        </div>
        <span class="text-xs text-text-4">A package is glob patterns over skill names in skills.yml. Names come from the skills Hive ships and the SKILL.md files under .shared/skills.</span>
        <p v-if="skillPackagesProblem" class="text-xs text-severity-error" data-testid="agent-workspace-editor-skills-problem">{{ skillPackagesProblem }}</p>
        <p v-if="skillError" class="text-xs text-severity-error" data-testid="agent-workspace-editor-skill-error">{{ skillError }}</p>
      </div>

      <!-- Schedules are manifest state like the lists above, so they are part
           of this form and travel with its Save. Only Run now and the run
           history talk to the Go side from in here. -->
      <div class="flex flex-col gap-1.5" data-testid="agent-workspace-editor-schedules">
        <span class="text-xs text-text-3">Schedules</span>

        <div v-if="scheduleCards.length" class="flex flex-col gap-2">
          <div
            v-for="(card, index) in scheduleCards"
            :key="card.key"
            class="flex flex-col rounded-lg border border-strong bg-raised"
            :data-testid="`agent-workspace-editor-schedule-${index}`"
          >
            <div class="flex items-start gap-2.5 px-3 py-2.5">
              <AppSwitch
                size="sm"
                class="mt-0.5"
                :model-value="!card.disabled"
                :aria-label="card.disabled ? 'Enable this schedule' : 'Pause this schedule'"
                :disabled="busy"
                :testid="`agent-workspace-editor-schedule-${index}-enabled`"
                @update:model-value="(enabled) => (card.disabled = !enabled)"
              />
              <div class="min-w-0 flex-1">
                <div class="flex min-w-0 items-center gap-1.5">
                  <span class="min-w-0 truncate text-[13px]" :class="card.disabled ? 'text-text-3' : 'text-text'">{{ scheduleLabel(card) }}</span>
                  <span
                    v-if="card.lastRun"
                    class="shrink-0 rounded-full border px-1.5 py-px text-[10px]"
                    :class="statusClasses(card.lastRun.status)"
                    :data-testid="`agent-workspace-editor-schedule-${index}-last-run`"
                  >{{ card.lastRun.status }} · {{ reasonLabel(card.lastRun) }}</span>
                </div>
                <div class="mt-0.5 flex min-w-0 items-center gap-1.5 text-[11px] text-text-4">
                  <span class="min-w-0 truncate" :data-testid="`agent-workspace-editor-schedule-${index}-summary`">{{ describe(card.shape) }}</span>
                  <span aria-hidden="true">·</span>
                  <span class="shrink-0" :data-testid="`agent-workspace-editor-schedule-${index}-next`">{{ nextRunLabel(card) }}</span>
                </div>
                <p v-if="card.lastRun?.error" class="mt-0.5 text-[11px] leading-relaxed text-severity-warning">{{ card.lastRun.error }}</p>
                <p
                  v-if="scheduleProblem(card)"
                  class="mt-0.5 text-[11px] leading-relaxed text-severity-error"
                  :data-testid="`agent-workspace-editor-schedule-${index}-problem`"
                >{{ scheduleProblem(card) }}</p>
                <p
                  v-if="card.actionError"
                  class="mt-0.5 text-[11px] leading-relaxed text-severity-error"
                  :data-testid="`agent-workspace-editor-schedule-${index}-action-error`"
                >{{ card.actionError }}</p>
              </div>
              <div class="flex shrink-0 items-center gap-1">
                <button
                  v-if="card.saved"
                  type="button"
                  class="flex size-6 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text disabled:cursor-default disabled:opacity-40"
                  title="Run now"
                  aria-label="Run now"
                  :disabled="busy || card.running"
                  :data-testid="`agent-workspace-editor-schedule-${index}-run`"
                  @click="runScheduleNow(card)"
                ><IconPlay class="size-3" /></button>
                <button
                  type="button"
                  class="flex size-6 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
                  :title="card.expanded ? 'Close this schedule' : 'Edit this schedule'"
                  :aria-label="card.expanded ? 'Close this schedule' : 'Edit this schedule'"
                  :aria-expanded="card.expanded"
                  :data-testid="`agent-workspace-editor-schedule-${index}-edit`"
                  @click="toggleScheduleExpanded(card)"
                ><IconPencil class="size-3" /></button>
                <button
                  type="button"
                  class="flex size-6 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-severity-error"
                  title="Remove this schedule"
                  aria-label="Remove this schedule"
                  :data-testid="`agent-workspace-editor-schedule-${index}-remove`"
                  @click="card.removing = true"
                ><IconTrash2 class="size-3" /></button>
              </div>
            </div>

            <div v-if="card.expanded" class="flex flex-col gap-3 border-t border-row px-3 py-3">
              <TextField
                :model-value="card.name"
                label="Name"
                :placeholder="card.saved ? card.id : 'Weekly product summary'"
                :hint="card.saved ? 'Optional. The id is what the schedule is called when this is empty.' : ''"
                :disabled="busy"
                :testid="`agent-workspace-editor-schedule-${index}-name`"
                @update:model-value="(value) => (card.name = value)"
              />

              <SelectField
                :model-value="card.shape.kind"
                label="Repeat"
                :options="REPEAT_OPTIONS"
                :disabled="busy"
                :testid="`agent-workspace-editor-schedule-${index}-repeat`"
                @update:model-value="(kind) => setRepeat(card, kind)"
              />

              <div v-if="card.shape.kind === 'weekly'" class="flex flex-col gap-1.5">
                <span class="text-[12.5px] text-text-2">Days</span>
                <div class="flex flex-wrap gap-1">
                  <button
                    v-for="day in WEEK_ORDER"
                    :key="day"
                    type="button"
                    class="cursor-pointer rounded-[7px] border px-2 py-1 text-[11.5px]"
                    :class="dayPicked(card, day) ? 'border-accent bg-accent text-accent-contrast' : 'border-card text-text-2 hover:text-text'"
                    :aria-pressed="dayPicked(card, day)"
                    :data-testid="`agent-workspace-editor-schedule-${index}-day-${day}`"
                    @click="toggleDay(card, day)"
                  >{{ dayAbbreviation(day) }}</button>
                </div>
              </div>

              <SelectField
                v-if="card.shape.kind === 'monthly'"
                :model-value="String(card.shape.day)"
                label="Day of the month"
                :options="MONTH_DAY_OPTIONS"
                :disabled="busy"
                hint="A day past the 28th is skipped in months that are shorter."
                :testid="`agent-workspace-editor-schedule-${index}-month-day`"
                @update:model-value="(value) => setMonthDay(card, value)"
              />

              <template v-if="card.shape.kind === 'hourly'">
                <SelectField
                  :model-value="minuteSelection(card)"
                  label="Minute"
                  :options="MINUTE_OPTIONS"
                  :disabled="busy"
                  :testid="`agent-workspace-editor-schedule-${index}-minute`"
                  @update:model-value="(value) => setMinuteSelection(card, value)"
                />
                <input
                  v-if="card.minuteFree"
                  type="number"
                  min="0"
                  max="59"
                  :value="card.shape.minute"
                  :disabled="busy"
                  aria-label="Minute past the hour"
                  class="w-24 rounded-lg border border-strong bg-app px-3 py-2 text-[13.5px] text-text outline-none focus:border-accent"
                  :data-testid="`agent-workspace-editor-schedule-${index}-minute-free`"
                  @input="onMinuteInput(card, $event)"
                >
              </template>

              <div v-if="card.shape.kind !== 'hourly' && card.shape.kind !== 'custom'" class="flex flex-col gap-1.5">
                <span class="text-[12.5px] text-text-2">Time</span>
                <input
                  type="time"
                  step="60"
                  :value="timeValue(card.shape)"
                  :disabled="busy"
                  aria-label="Time"
                  class="w-36 rounded-lg border border-strong bg-app px-3 py-2 text-[13.5px] text-text outline-none focus:border-accent"
                  :data-testid="`agent-workspace-editor-schedule-${index}-time`"
                  @input="onTimeInput(card, $event)"
                >
              </div>

              <TextField
                v-if="card.shape.kind === 'custom'"
                :model-value="card.shape.cron"
                label="Cron"
                monospace
                placeholder="0 9 * * 5"
                :disabled="busy"
                :error="card.cronError"
                hint="Five fields in local time, or @hourly / @daily / @weekly / @monthly."
                :testid="`agent-workspace-editor-schedule-${index}-cron`"
                @update:model-value="(value) => setCron(card, value)"
              />

              <SelectField
                :model-value="card.onMissed"
                label="When missed"
                :options="ON_MISSED_OPTIONS"
                :disabled="busy"
                :testid="`agent-workspace-editor-schedule-${index}-on-missed`"
                @update:model-value="(value) => (card.onMissed = value)"
              />

              <TextareaField
                :model-value="card.prompt"
                label="Prompt"
                :rows="5"
                monospace
                placeholder="Summarize what changed since the last run."
                :error="card.promptError"
                :hint="promptHint"
                :testid="`agent-workspace-editor-schedule-${index}-prompt`"
                @update:model-value="(value) => setPrompt(card, value)"
              />

              <div class="flex flex-col gap-1">
                <span class="text-[12.5px] text-text-2">Next runs</span>
                <ul
                  v-if="card.next.length"
                  class="flex flex-col gap-0.5"
                  :data-testid="`agent-workspace-editor-schedule-${index}-next-runs`"
                >
                  <li v-for="at in card.next" :key="at" class="font-mono text-[11.5px] text-text-3">{{ occurrence(at) }}</li>
                </ul>
                <p v-else class="text-[11.5px] text-text-4">
                  {{ card.cronError ? 'Nothing to show while the cron is invalid.' : 'No upcoming runs.' }}
                </p>
              </div>
            </div>

            <div v-if="card.saved" class="border-t border-row px-3 py-2">
              <button
                type="button"
                class="flex cursor-pointer items-center gap-1 text-[11.5px] text-text-4 hover:text-text-3"
                :aria-expanded="card.historyOpen"
                :data-testid="`agent-workspace-editor-schedule-${index}-history`"
                @click="toggleHistory(card)"
              >
                Recent runs
                <IconChevronDown class="size-3 transition-transform" :class="{ '-rotate-90': !card.historyOpen }" />
              </button>
              <template v-if="card.historyOpen">
                <p v-if="card.historyError" class="mt-1 text-[11px] text-severity-error">{{ card.historyError }}</p>
                <p v-else-if="!card.history.length" class="mt-1 text-[11px] text-text-4">No runs yet.</p>
                <div v-else class="mt-1 flex flex-col divide-y divide-row">
                  <div v-for="run in card.history" :key="run.id" class="flex items-start gap-2 py-1.5">
                    <span class="mt-px shrink-0 font-mono text-[10.5px] text-text-4" :title="new Date(run.startedAt).toLocaleString()">{{ runStamp(run.startedAt) }}</span>
                    <div class="min-w-0 flex-1">
                      <div class="flex min-w-0 items-center gap-1.5">
                        <span
                          class="shrink-0 rounded-full border px-1.5 py-px text-[10px]"
                          :class="statusClasses(run.status)"
                        >{{ run.status }} · {{ reasonLabel(run) }}</span>
                        <span v-if="run.missed > 0" class="shrink-0 font-mono text-[10px] text-text-4">{{ run.missed }} missed</span>
                      </div>
                      <p v-if="run.error" class="mt-0.5 text-[10.5px] leading-relaxed text-severity-error">{{ run.error }}</p>
                    </div>
                    <button
                      v-if="run.sessionId"
                      type="button"
                      class="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
                      title="Open chat"
                      aria-label="Open chat"
                      :data-testid="`agent-workspace-editor-schedule-open-chat-${run.id}`"
                      @click="emit('open-chat', run.sessionId)"
                    ><IconMessageSquare class="size-3" /></button>
                  </div>
                </div>
              </template>
            </div>

            <div
              v-if="card.removing"
              class="flex items-center gap-2 border-t border-row bg-severity-error-tint px-3 py-2"
              :data-testid="`agent-workspace-editor-schedule-${index}-remove-confirm`"
            >
              <span class="min-w-0 flex-1 text-[11.5px] leading-relaxed text-severity-error">Remove this schedule? It leaves the manifest when you save.</span>
              <BaseButton
                variant="danger-outline"
                size="sm"
                :data-testid="`agent-workspace-editor-schedule-${index}-remove-confirm-yes`"
                @click="removeSchedule(card)"
              >Remove</BaseButton>
              <BaseButton variant="secondary" size="sm" @click="card.removing = false">Keep</BaseButton>
            </div>
          </div>
        </div>

        <button
          type="button"
          class="flex cursor-pointer items-center gap-1.5 self-start text-[12px] text-accent hover:underline"
          :disabled="busy"
          data-testid="agent-workspace-editor-schedule-add"
          @click="addSchedule"
        ><IconPlus class="size-3.5" />Add schedule</button>
        <span class="text-xs text-text-4">A schedule starts a chat in this workspace on its own timetable. Its prompt is a Go template, so one schedule can ask for everything since the last run.</span>
      </div>

      <p v-if="!creating" class="text-xs leading-relaxed text-text-4">
        Saving rewrites these fields in agent-workspace.yaml and re-syncs the workspace's
        generated files. Comments and anything else in the file stay as written — edit the
        file for those.
      </p>
      <p
        v-if="error && !confirming"
        class="rounded border border-severity-error bg-severity-error-tint px-3 py-2 text-xs text-severity-error"
        data-testid="agent-workspace-editor-error"
      >{{ error }}</p>
    </div>

    <template #footer>
      <!-- The strip escapes the footer's own padding so it reads as the
           sheet's bottom edge, the way InlineConfirm is designed to sit. -->
      <InlineConfirm
        v-if="confirming"
        class="-mx-[18px] -my-[13px]"
        title="Delete this workspace?"
        :description="`Every live chat in ${workspace!.name || workspace!.dir} closes, its chat history is removed, and ${deletedPath} is deleted from disk with everything in it — AGENTS.md, docs, canvases, and anything else written there. This cannot be undone.`"
        confirm-label="Delete"
        :busy="busy"
        :error="error"
        testid="agent-workspace-editor-delete-confirm"
        @confirm="emit('delete', workspace!.dir)"
        @cancel="confirming = false"
      />
      <div v-else class="flex items-center gap-2.5">
        <button
          v-if="!creating"
          type="button"
          class="cursor-pointer text-[12.5px] text-text-3 hover:text-severity-error disabled:opacity-50"
          :disabled="busy"
          data-testid="agent-workspace-editor-delete"
          @click="confirming = true"
        >Delete workspace</button>
        <div class="flex-1" />
        <BaseButton variant="secondary" size="sm" :busy="busy" data-testid="agent-workspace-editor-cancel" @click="cancel">Cancel</BaseButton>
        <BaseButton size="sm" :busy="busy" :disabled="!valid" data-testid="agent-workspace-editor-save" @click="submit">
          {{ creating ? 'Create workspace' : 'Save' }}
        </BaseButton>
      </div>
    </template>
  </DrawerSheet>
</template>
