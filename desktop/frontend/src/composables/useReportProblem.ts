import { ref } from 'vue'
import { Browser } from '@wailsio/runtime'
import { Preview, Save } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/reportservice'
import { OpenPath } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/systemservice'
import type { ReportInput, ReportPreview, ReportResult } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/models'

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// Save writes a file and opens a form. Nothing uploads the bundle, here or in
// the backend, and nothing should.
export function useReportProblem() {
  const preview = ref<ReportPreview | null>(null)
  const loading = ref(false)
  const saving = ref(false)
  const error = ref('')
  const saved = ref<ReportResult | null>(null)

  async function loadPreview(): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      preview.value = await Preview()
    } catch (err) {
      error.value = errText(err)
    } finally {
      loading.value = false
    }
  }

  async function save(input: ReportInput): Promise<boolean> {
    saving.value = true
    error.value = ''
    try {
      const res = await Save(input)
      saved.value = res
      await Browser.OpenURL(res.issueUrl)
      return true
    } catch (err) {
      error.value = errText(err)
      return false
    } finally {
      saving.value = false
    }
  }

  // The reports directory is one of the app's known locations, so this needs
  // no reveal binding of its own.
  async function openFolder(): Promise<void> {
    if (!saved.value) return
    error.value = ''
    try {
      await OpenPath(saved.value.dir)
    } catch (err) {
      error.value = errText(err)
    }
  }

  async function openIssue(): Promise<void> {
    if (!saved.value) return
    error.value = ''
    try {
      await Browser.OpenURL(saved.value.issueUrl)
    } catch (err) {
      error.value = errText(err)
    }
  }

  return { preview, loading, saving, error, saved, loadPreview, save, openFolder, openIssue }
}
