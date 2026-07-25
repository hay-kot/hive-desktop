import { ref } from 'vue'
import { Catalog, Render } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/promptsservice'
import type { Prompt } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/prompts/models'
import { commandCatalog } from '../keybindings/catalog'

// The frontend never builds prompt text. Every prompt — the settings catalog
// and the context-scoped ones offered from an editor — is assembled by
// internal/app/prompts from templates that compose shared fragments, so
// wording is written once and each prompt names this install's real config
// paths rather than a placeholder.
//
// This module is the transport. Its one job beyond calling the service is
// supplying the single fact Go cannot know: the bindable command catalog,
// which stays in keybindings/catalog.ts because it carries icons and palette
// grouping. Adding a command there extends the keyboard-shortcuts prompt with
// no change here or in Go.

/** The bindable command list, reduced to the fields the prompt renders. */
function promptInput() {
  return {
    commands: commandCatalog.map((command) => ({
      id: command.id,
      title: command.title,
      group: command.group,
      context: command.context,
      defaultCombos: command.defaultCombos,
    })),
    webhookPath: '',
    webhookSample: '',
  }
}

/** The settings catalog: every prompt that needs no per-instance context. */
export function usePromptCatalog() {
  const prompts = ref<Prompt[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function refresh(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      prompts.value = (await Catalog(promptInput())) ?? []
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Could not load prompts.'
      prompts.value = []
    } finally {
      loading.value = false
    }
  }

  return { prompts, loading, error, refresh }
}

/**
 * Renders one prompt by id. Context-scoped prompts (a webhook node's transform
 * prompt) pass their instance data through `context`; catalog prompts pass
 * nothing. Returns null on failure so callers can surface a copy error rather
 * than putting a half-built prompt on the clipboard.
 */
export async function renderPrompt(
  id: string,
  context: { webhookPath?: string; webhookSample?: string } = {},
): Promise<string | null> {
  try {
    const prompt = await Render(id, {
      ...promptInput(),
      webhookPath: context.webhookPath ?? '',
      webhookSample: context.webhookSample ?? '',
    })
    return prompt.text
  } catch (error) {
    console.warn(`Unable to render the "${id}" prompt`, error)
    return null
  }
}
