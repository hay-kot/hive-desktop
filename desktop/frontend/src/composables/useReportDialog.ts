import { ref } from 'vue'

// Module-scoped so the System settings entry and the command palette open the
// same dialog, which is mounted once at the app root.
const open = ref(false)

export function useReportDialog() {
  return {
    open,
    openDialog: () => { open.value = true },
    close: () => { open.value = false },
  }
}
