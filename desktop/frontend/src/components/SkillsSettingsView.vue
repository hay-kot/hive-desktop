<script setup lang="ts">
// The one place that installs Hive's configuration skills into the folders a
// coding agent reads. Management is per agent, all-or-nothing: an agent roster on
// top whose toggle installs every skill to that agent or removes them all, and an
// informational list below of what gets installed, with preview + copy per skill
// for an agent with no known folder. Sync keeps every installed agent current.
//
// The list is whatever the skills service reports, so adding a prompt in Go adds a
// skill here with no edit. This component builds no skill text of its own.
import { computed, onMounted, onScopeDispose, ref } from 'vue'
import IconCheck from '~icons/lucide/check'
import IconChevronDown from '~icons/lucide/chevron-down'
import IconCopy from '~icons/lucide/copy'
import IconRefreshCw from '~icons/lucide/refresh-cw'
import BaseButton from './BaseButton.vue'
import AppSwitch from './AppSwitch.vue'
import AgentIcon, { agentHasIcon } from './AgentIcon.vue'
import { useClipboard } from '../composables/useClipboard'
import { useSkills } from '../composables/useSkills'
import type { SkillTarget } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/models'

const { catalog, loading, error, busy, refresh, installTarget, uninstallTarget, sync, setTargetDir, setAutoUpdate } = useSkills()
const { copy, status: copyStatus } = useClipboard({ resetDelay: 2500 })

const expandedId = ref<string | null>(null)
const copiedId = ref<string | null>(null)
const lastSync = ref<string | null>(null)

// A transient result line, so every action visibly does something.
const status = ref<string | null>(null)
let statusTimer: ReturnType<typeof setTimeout> | undefined
function flash(message: string): void {
  status.value = message
  if (statusTimer) clearTimeout(statusTimer)
  statusTimer = setTimeout(() => { status.value = null }, 3500)
}
onScopeDispose(() => { if (statusTimer) clearTimeout(statusTimer) })

const skills = computed(() => catalog.value?.skills ?? [])
const targets = computed(() => catalog.value?.targets ?? [])
const installedAgents = computed(() => targets.value.filter((t) => t.installed > 0).length)
const needsSyncCount = computed(() => targets.value.filter((t) => t.needsSync).length)

function pending(key: string): boolean {
  return busy.value === key
}

function plural(n: number): string {
  return n === 1 ? '' : 's'
}

// The roster toggle is the whole install control: on installs every skill to the
// agent, off removes them all (a file you edited is left in place and reported).
async function onToggleTarget(target: SkillTarget, value: boolean): Promise<void> {
  const result = value ? await installTarget(target.id) : await uninstallTarget(target.id)
  if (!result) return
  if (value) {
    flash(`Installed ${result.count} skill${plural(result.count)} to ${target.label}`)
    return
  }
  let message = `Removed ${result.count} skill${plural(result.count)} from ${target.label}`
  if (result.kept > 0) message += ` · kept ${result.kept} you edited`
  flash(message)
}

async function onSync(): Promise<void> {
  const result = await sync()
  if (!result) return
  lastSync.value = new Date().toLocaleTimeString()
  const changed = result.installed + result.updated + result.restored
  if (changed === 0) {
    flash('Everything up to date')
    return
  }
  const parts: string[] = []
  if (result.installed) parts.push(`installed ${result.installed}`)
  if (result.updated) parts.push(`updated ${result.updated}`)
  if (result.restored) parts.push(`restored ${result.restored}`)
  flash(parts.join(' · '))
}

function toggle(id: string): void {
  expandedId.value = expandedId.value === id ? null : id
}

function justCopied(id: string): boolean {
  return copiedId.value === id && copyStatus.value === 'success'
}

function copyFailed(id: string): boolean {
  return copiedId.value === id && copyStatus.value === 'error'
}

async function onCopy(id: string, text: string): Promise<void> {
  copiedId.value = id
  await copy(text)
}

function onDirChange(targetID: string, event: Event): void {
  void setTargetDir(targetID, (event.target as HTMLInputElement).value)
}

// A short mono badge for an agent with no icon — "CC", "OC", "PI", "AS".
function initials(label: string): string {
  const words = label.replace(/[()]/g, '').trim().split(/\s+/)
  if (words[0] && words[0].length <= 3) return words[0].slice(0, 2).toUpperCase()
  return ((words[0]?.[0] ?? '') + (words[1]?.[0] ?? '')).toUpperCase()
}

onMounted(() => void refresh())
</script>

<template>
  <div class="mx-auto flex max-w-[820px] flex-col gap-5" data-testid="skill-settings">
    <!-- header -->
    <div class="flex flex-wrap items-start gap-3">
      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-baseline gap-2.5">
          <h2 class="text-[16px] font-semibold tracking-[-.01em] text-text">Skills</h2>
          <span v-if="catalog" class="font-mono text-[11.5px] text-text-4">
            {{ skills.length }} skills · {{ installedAgents }} / {{ targets.length }} agents installed
          </span>
        </div>
        <p class="mt-1 max-w-[560px] text-[12.5px] leading-relaxed text-text-3">
          Hive writes its configuration prompts as SKILL.md files where your agents read them. Turn on an agent to install all of them; Sync keeps installed agents current. Files you edit yourself are never overwritten.
        </p>
      </div>
      <div v-if="catalog" class="flex shrink-0 items-center gap-3">
        <span v-if="status" class="text-[11.5px] font-medium text-accent" data-testid="skill-status">{{ status }}</span>
        <AppSwitch
          :model-value="catalog.autoUpdate"
          size="sm"
          label="Auto-sync"
          aria-label="Keep installed skills current on startup"
          testid="skill-autoupdate"
          @update:model-value="setAutoUpdate"
        />
        <BaseButton size="sm" variant="primary" :disabled="pending('sync')" data-testid="skill-sync" @click="onSync">
          <template #icon><IconRefreshCw class="size-3.5" :class="pending('sync') ? 'animate-spin' : ''" /></template>
          Sync
        </BaseButton>
      </div>
    </div>

    <p v-if="loading" class="text-xs text-text-4" data-testid="skill-settings-loading">Loading skills…</p>
    <p
      v-else-if="error"
      class="rounded border border-severity-error bg-severity-error-tint px-3 py-2 text-xs text-severity-error"
      data-testid="skill-settings-error"
    >{{ error }}</p>

    <template v-if="!loading && catalog">
      <!-- agent roster -->
      <section class="flex flex-col gap-2">
        <div class="flex items-baseline gap-2.5">
          <span class="font-mono text-[10.5px] tracking-[.14em] text-text-4">AGENTS</span>
          <div class="h-px flex-1 bg-border"></div>
          <span class="text-[11.5px] text-text-4">turn on to install every skill</span>
        </div>
        <div class="overflow-hidden rounded-lg border border-border bg-raised">
          <div
            class="grid items-center gap-3 border-b border-border bg-chip px-3.5 py-2 font-mono text-[10px] tracking-[.12em] text-text-4"
            style="grid-template-columns: 34px minmax(120px, 190px) minmax(0, 1fr) 96px"
          >
            <span></span><span>AGENT</span><span>SKILLS DIRECTORY</span><span class="text-right">INSTALLED</span>
          </div>
          <div
            v-for="target in targets"
            :key="target.id"
            class="grid items-center gap-3 border-b border-border px-3.5 py-2 last:border-b-0"
            style="grid-template-columns: 34px minmax(120px, 190px) minmax(0, 1fr) 96px"
            :data-testid="`skill-target-${target.id}`"
          >
            <AppSwitch
              :model-value="target.installed > 0"
              :aria-label="`Install all skills to ${target.label}`"
              :disabled="pending(`target:${target.id}`)"
              :testid="`skill-target-${target.id}-toggle`"
              @update:model-value="(value) => onToggleTarget(target, value)"
            />
            <span class="flex min-w-0 items-center gap-2">
              <span class="flex size-5 shrink-0 items-center justify-center rounded border border-border bg-chip text-text-2" :title="target.label">
                <AgentIcon v-if="agentHasIcon(target.id)" :id="target.id" class="size-3" />
                <span v-else class="font-mono text-[9px] font-semibold text-text-3">{{ initials(target.label) }}</span>
              </span>
              <span class="truncate text-[13px] text-text">{{ target.label }}</span>
            </span>
            <div class="flex h-7 items-center gap-2 rounded-md border border-border bg-canvas px-2.5">
              <input
                type="text"
                class="min-w-0 flex-1 bg-transparent font-mono text-[11px] text-text-2 outline-none"
                :value="target.dir"
                :placeholder="target.dir"
                :disabled="pending(`target:${target.id}`)"
                :data-testid="`skill-target-${target.id}-dir`"
                @change="onDirChange(target.id, $event)"
              />
              <span v-if="target.default" class="shrink-0 font-mono text-[9.5px] tracking-wide text-text-4">DEFAULT</span>
              <button
                v-else
                type="button"
                class="shrink-0 cursor-pointer font-mono text-[9.5px] tracking-wide text-accent hover:underline"
                :data-testid="`skill-target-${target.id}-reset`"
                @click="setTargetDir(target.id, '')"
              >RESET</button>
            </div>
            <span class="flex items-center justify-end gap-1.5 text-right font-mono text-[11px]" :data-testid="`skill-target-${target.id}-count`">
              <span
                v-if="target.needsSync"
                class="rounded bg-severity-warning-tint px-1 py-0.5 text-[9px] tracking-wide text-severity-warning"
                title="Some skills are missing or out of date — Sync will fix them"
                :data-testid="`skill-target-${target.id}-needssync`"
              >SYNC</span>
              <span class="text-text-3">{{ target.installed }} / {{ skills.length }}</span>
            </span>
          </div>
        </div>
      </section>

      <!-- skills list -->
      <section class="mt-3 flex flex-col gap-2">
        <div class="flex items-baseline gap-2.5">
          <span class="font-mono text-[10.5px] tracking-[.14em] text-text-4">SKILLS</span>
          <div class="h-px flex-1 bg-border"></div>
          <span class="text-[11.5px] text-text-4">what each agent gets</span>
        </div>
        <div class="flex flex-col gap-2">
          <div
            v-for="skill in skills"
            :key="skill.id"
            class="overflow-hidden rounded-lg border border-border bg-raised"
            :data-testid="`skill-${skill.id}`"
          >
            <div class="flex items-start gap-3 px-3.5 py-2.5">
              <div class="min-w-0 flex-1">
                <div class="text-[13px] font-medium text-text">{{ skill.title }}</div>
                <div class="mt-0.5 text-[11.5px] leading-relaxed text-text-3">{{ skill.description }}</div>
                <div v-if="skill.target" class="mt-0.5 truncate font-mono text-[10.5px] text-text-4" :title="skill.target">{{ skill.target }}</div>
              </div>
              <button
                type="button"
                class="flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-[11.5px] font-medium text-text-3 hover:bg-chip hover:text-text"
                :aria-expanded="expandedId === skill.id"
                :data-testid="`skill-${skill.id}-preview`"
                @click="toggle(skill.id)"
              >
                Preview
                <IconChevronDown class="size-3.5 transition-transform" :class="expandedId === skill.id ? 'rotate-180' : ''" />
              </button>
            </div>

            <div v-if="expandedId === skill.id" class="border-t border-border bg-canvas px-3.5 py-3" :data-testid="`skill-${skill.id}-panel`">
              <div class="mb-2 flex items-center gap-2">
                <span class="min-w-0 flex-1 truncate font-mono text-[10.5px] text-text-4">{{ skill.name }} · SKILL.md</span>
                <BaseButton size="sm" variant="secondary" :data-testid="`skill-${skill.id}-copy`" @click="onCopy(skill.id, skill.text)">
                  <template #icon>
                    <IconCheck v-if="justCopied(skill.id)" class="size-3.5" :stroke-width="2.4" />
                    <IconCopy v-else class="size-3.5" :stroke-width="2.2" />
                  </template>
                  <span class="grid text-center">
                    <span class="invisible col-start-1 row-start-1">Copied</span>
                    <span class="col-start-1 row-start-1" :data-testid="`skill-${skill.id}-copy-label`">{{ justCopied(skill.id) ? 'Copied' : 'Copy' }}</span>
                  </span>
                </BaseButton>
              </div>
              <p v-if="copyFailed(skill.id)" class="mb-2 text-[11.5px] text-severity-error" :data-testid="`skill-${skill.id}-copy-error`">Could not copy to the clipboard.</p>
              <pre
                class="hive-scroll max-h-[300px] overflow-auto rounded-md border border-border bg-raised p-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap text-text-2"
                :data-testid="`skill-${skill.id}-text`"
              >{{ skill.text }}</pre>
            </div>
          </div>
        </div>
      </section>

      <!-- footer -->
      <div class="flex flex-wrap items-center gap-x-4 gap-y-1 font-mono text-[10.5px] tracking-wide text-text-4">
        <span v-if="lastSync" data-testid="skill-last-sync">LAST SYNC {{ lastSync }}</span>
        <div class="flex-1"></div>
        <span v-if="needsSyncCount" class="text-severity-warning" data-testid="skill-needssync-count">{{ needsSyncCount }} AGENT{{ needsSyncCount === 1 ? '' : 'S' }} NEED SYNC</span>
        <span>YOUR OWN EDITS ARE NEVER OVERWRITTEN</span>
      </div>
    </template>
  </div>
</template>
