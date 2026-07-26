<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Browser } from '@wailsio/runtime'
import IconAlertTriangle from '~icons/lucide/alert-triangle'
import IconCheck from '~icons/lucide/check'
import IconGithub from '~icons/lucide/github'
import IconLayoutGrid from '~icons/lucide/layout-grid'
import type { DeviceFlowInfo } from '../types/github'
import type { ConnectCard } from '../composables/useGitHubConnection'
import { useClipboard } from '../composables/useClipboard'

const props = defineProps<{
  // 'workspace' is step 1: the workspace is the thing that exists before any
  // credential does. The connect cards are step 2, and are skippable.
  card: ConnectCard | 'workspace'
  deviceFlow: DeviceFlowInfo | null
  error: string | null
  busy: boolean
}>()

const emit = defineEmits<{
  startDeviceFlow: []
  useTokenInstead: []
  backToStart: []
  submitToken: [token: string]
  createWorkspace: [name: string]
  skipConnect: []
}>()

const tokenInput = ref('')
const workspaceInput = ref('')
const { copy, copied } = useClipboard({ resetDelay: 1600 })

// Confirming the skip is local to this screen: it is a warning to read, not a
// state the app has to hold. Leaving the connect step at all drops it.
const confirmingSkip = ref(false)
watch(() => props.card, () => { confirmingSkip.value = false })

// Two steps, because onboarding has two screens. A third "add feeds & tasks"
// entry used to sit here and could never become active — the app leaves this
// screen after the last card — and it is not even a user task now: connecting
// is what seeds the workspace's feeds.
const activeStep = computed(() => props.card === 'workspace' ? 1 : 2)
const steps = [
  { label: 'Create your first workspace', step: 1 },
  { label: 'Connect GitHub', step: 2 },
]

async function openVerification() {
  const uri = props.deviceFlow?.verificationUri
  if (!uri) return
  try {
    await Browser.OpenURL(uri)
  } catch {
    window.open(uri, '_blank')
  }
}

function copyCode() {
  const code = props.deviceFlow?.userCode
  if (!code) return
  void copy(code)
}

function submit() {
  if (props.busy) return
  if (tokenInput.value.trim()) emit('submitToken', tokenInput.value.trim())
}

function submitWorkspace() {
  if (props.busy) return
  if (workspaceInput.value.trim()) emit('createWorkspace', workspaceInput.value.trim())
}
</script>

<template>
  <div class="flex min-h-0 flex-1" data-testid="onboarding">
    <!-- Left: brand panel with onboarding steps -->
    <aside class="flex w-[470px] shrink-0 flex-col border-r border-border bg-raised px-11 py-12">
      <div class="mb-10 flex items-center gap-3">
        <div class="flex size-[38px] items-center justify-center rounded-[11px] bg-accent-tint font-mono text-[17px] font-bold text-accent">h</div>
        <span class="font-mono text-[17px] font-semibold">hive</span>
      </div>
      <h1 class="mb-3 text-[26px] font-semibold leading-[1.25] tracking-[-.02em]">Triage GitHub and<br>spin up sessions.</h1>
      <p class="mb-11 max-w-[330px] text-sm leading-relaxed text-text-3">Name a workspace, then connect the account it pulls PRs, issues, and notifications from.</p>
      <ol class="flex flex-col gap-5">
        <li v-for="step in steps" :key="step.label" class="flex items-center gap-3.5">
          <span
            class="flex size-[30px] shrink-0 items-center justify-center rounded-full text-[13px] font-semibold"
            :class="step.step === activeStep ? 'bg-accent text-accent-contrast' : step.step < activeStep ? 'border border-accent-tint bg-chip text-accent' : 'border border-strong bg-chip text-text-3'"
          ><IconCheck v-if="step.step < activeStep" class="size-3.5" /><template v-else>{{ step.step }}</template></span>
          <span class="text-sm" :class="step.step === activeStep ? 'font-medium text-text' : 'text-text-3'">{{ step.label }}</span>
        </li>
      </ol>
      <div class="flex-1" />
      <p class="text-xs text-text-4">Tokens are stored in your OS keychain.</p>
    </aside>

    <!-- Right: connect card -->
    <section class="flex flex-1 items-center justify-center bg-pane p-10">
      <div class="w-[420px] text-center">
        <div class="mx-auto mb-5 flex size-[60px] items-center justify-center rounded-[15px] border border-strong bg-chip text-text">
          <IconAlertTriangle v-if="confirmingSkip" class="size-[30px]" />
          <IconLayoutGrid v-else-if="card === 'workspace'" class="size-[30px]" />
          <IconGithub v-else class="size-[30px]" />
        </div>
        <h2 class="mb-2 text-xl font-semibold tracking-[-.01em]">{{ confirmingSkip ? 'Skip connecting GitHub?' : card === 'workspace' ? 'Create your first workspace' : 'Connect to GitHub' }}</h2>

        <!-- skip: the warning the bypass goes past, not a gate -->
        <template v-if="confirmingSkip">
          <p class="mb-6 text-[13.5px] leading-relaxed text-text-3">This workspace will have no sources, so your feed stays empty until you connect an account under Settings ▸ Integrations.</p>
          <button
            class="primary-button"
            data-testid="onboarding-skip-confirm"
            @click="emit('skipConnect')"
          >Continue without GitHub</button>
          <p class="mt-4 text-xs text-text-4">
            <button class="link-quiet" data-testid="onboarding-skip-back" @click="confirmingSkip = false">Back to connecting</button>
          </p>
        </template>

        <!-- workspace: step 1, the one step that needs no credential -->
        <template v-else-if="card === 'workspace'">
          <p class="mb-6 text-[13.5px] leading-relaxed text-text-3">A workspace groups your feeds. Connect an account next and it starts with your open PRs, the notifications inbox, and cross-repo assignments.</p>
          <input
            v-model="workspaceInput"
            type="text"
            placeholder="Frontend Triage"
            class="mb-3 w-full rounded-lg border border-strong bg-app px-3.5 py-2.5 text-[13.5px] text-text outline-none placeholder:text-text-4 focus:border-accent"
            data-testid="onboarding-workspace-input"
            @keydown.enter="submitWorkspace"
          >
          <button
            class="primary-button"
            :disabled="busy || !workspaceInput.trim()"
            data-testid="onboarding-workspace-submit"
            @click="submitWorkspace"
          >Create workspace</button>
          <p v-if="error" class="mt-4 text-xs text-kind-issue" data-testid="onboarding-error">{{ error }}</p>
        </template>

        <!-- idle: not started -->
        <template v-else-if="card === 'idle'">
          <p class="mb-6 text-[13.5px] leading-relaxed text-text-3">Sign in from this device. Hive fills your workspace with your open PRs, your assignments, and the notifications inbox.</p>
          <button
            class="primary-button"
            :disabled="busy"
            data-testid="onboarding-connect"
            @click="emit('startDeviceFlow')"
          >Connect GitHub</button>
          <p v-if="error" class="mt-4 text-xs text-kind-issue" data-testid="onboarding-error">{{ error }}</p>
          <p class="mt-4 text-xs text-text-4">
            <button class="link-quiet" data-testid="onboarding-use-token" @click="emit('useTokenInstead')">Use a token instead</button>
          </p>
        </template>

        <!-- device: waiting for authorization -->
        <template v-else-if="card === 'device'">
          <p class="mb-6 text-[13.5px] leading-relaxed text-text-3">Open the link and enter this code to authorize Hive on your account.</p>
          <div class="mb-2.5 rounded-xl border border-strong bg-app px-4 py-[18px] font-mono text-[32px] font-semibold tracking-[.28em] text-accent" data-testid="onboarding-user-code">{{ deviceFlow?.userCode }}</div>
          <div class="mb-6 flex items-center justify-center gap-2 text-xs text-text-3">
            <span class="size-1.5 rounded-full bg-accent [animation:hivePulse_1.6s_ease-in-out_infinite]" />
            Waiting for authorization…
          </div>
          <button class="primary-button" data-testid="onboarding-open-verification" @click="openVerification">Open github.com/login/device ↗</button>
          <p v-if="error" class="mt-4 text-xs text-kind-issue" data-testid="onboarding-error">{{ error }}</p>
          <p class="mt-4 text-xs text-text-4">
            <button class="link-quiet" data-testid="onboarding-copy-code" @click="copyCode">{{ copied ? 'Copied' : 'Copy code' }}</button>
            ·
            <button class="link-quiet" data-testid="onboarding-use-token" @click="emit('useTokenInstead')">Use a token instead</button>
          </p>
        </template>

        <!-- token: personal access token fallback -->
        <template v-else>
          <p class="mb-6 text-[13.5px] leading-relaxed text-text-3">Paste a personal access token with <span class="font-mono text-text-2">repo</span> and <span class="font-mono text-text-2">notifications</span> scopes.</p>
          <input
            v-model="tokenInput"
            type="password"
            placeholder="ghp_…"
            class="mb-3 w-full rounded-lg border border-strong bg-app px-3.5 py-2.5 font-mono text-[13.5px] text-text outline-none placeholder:text-text-4 focus:border-accent"
            data-testid="onboarding-token-input"
            @keydown.enter="submit"
          >
          <button
            class="primary-button"
            :disabled="busy || !tokenInput.trim()"
            data-testid="onboarding-token-submit"
            @click="submit"
          >Save token</button>
          <p v-if="error" class="mt-4 text-xs text-kind-issue" data-testid="onboarding-error">{{ error }}</p>
          <p class="mt-4 text-xs text-text-4">
            <button class="link-quiet" data-testid="onboarding-back" @click="emit('backToStart')">Back to device sign-in</button>
          </p>
        </template>

        <!-- Bypassing GitHub is expected to be rare, so it sits below every
             connect card rather than beside the action that is the point. -->
        <p v-if="card !== 'workspace' && !confirmingSkip" class="mt-2.5 text-xs text-text-4">
          <button class="link-quiet" data-testid="onboarding-skip" @click="confirmingSkip = true">Skip for now</button>
        </p>
      </div>
    </section>
  </div>
</template>

<style scoped>
.primary-button {
  width: 100%;
  border-radius: 9px;
  background: var(--color-accent);
  color: var(--color-accent-contrast);
  padding: 12px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: filter .12s ease;
}
.primary-button:hover:not(:disabled) { filter: brightness(1.08); }
.primary-button:disabled { opacity: .55; cursor: default; }
.link-quiet { color: inherit; cursor: pointer; }
.link-quiet:hover { color: var(--color-text); }
</style>
