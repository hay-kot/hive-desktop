---
kind: changed
---

- **Report a problem files a GitHub issue.** The dialog now writes a diagnostic
  bundle to `reports/` in your data directory and opens the bug form with your
  version and platform filled in. Nothing is uploaded, and the bundle is yours
  to read before you attach it. Logs, settings, flows and actions are separate
  switches and all start off: secrets are stripped, but a log tail still names
  your home directory, repositories and branches.
