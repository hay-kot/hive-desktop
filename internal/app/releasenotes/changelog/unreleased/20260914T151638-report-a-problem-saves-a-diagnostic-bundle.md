---
kind: changed
---

- **Report a problem opens a GitHub issue.** It fills in your version and
  platform and attaches nothing else. Nothing is uploaded and no diagnostic
  bundle is built.
- **Saving a diagnostic bundle is now its own command.** Use it when a
  maintainer asks for one. Logs, settings, flows and actions are separate
  switches and all start off. Secrets are stripped, but a bundle still names
  your home directory, repositories and branches, so send it privately rather
  than attaching it to an issue.
