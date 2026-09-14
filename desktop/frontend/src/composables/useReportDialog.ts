import { ref } from 'vue'
import { Browser } from '@wailsio/runtime'
import { IssueURL } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/reportservice'

// Two separate actions, deliberately. "Report a problem" opens a public issue
// and carries only the build identity. The bundle dialog writes a file that
// names the user's paths, hosts and repositories, and it never goes to GitHub.
const bundleOpen = ref(false)

export function useReportDialog() {
  return {
    open: bundleOpen,
    openBundleDialog: () => { bundleOpen.value = true },
    close: () => { bundleOpen.value = false },
    reportProblem: async () => { await Browser.OpenURL(await IssueURL()) },
  }
}
