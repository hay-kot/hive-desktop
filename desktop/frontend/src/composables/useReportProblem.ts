import { ref } from 'vue'
import { Preview, Submit } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/reportservice'
import type { ReportInput, ReportPreview } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/models'

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

export function useReportProblem() {
  const preview = ref<ReportPreview | null>(null)
  const loading = ref(false)
  const submitting = ref(false)
  const error = ref('')
  const reportId = ref('')

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

  async function submit(input: ReportInput): Promise<boolean> {
    submitting.value = true
    error.value = ''
    try {
      const res = await Submit(input)
      reportId.value = res.id
      return true
    } catch (err) {
      error.value = errText(err)
      return false
    } finally {
      submitting.value = false
    }
  }

  return { preview, loading, submitting, error, reportId, loadPreview, submit }
}
