---
kind: fixed
---

**A session that fails to start keeps what you typed.** The New Session form comes back with the repository, name, prompt and agent you submitted, and names the step that failed, git's own reason for failing, and the checkout left on disk. The failure raises an error toast with a Retry, and lands in Activity with a Retry of its own that still works after a restart.
