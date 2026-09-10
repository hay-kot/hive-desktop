---
summary: ""
---

## Added

- **Hive can read an env file at startup.** A GUI launch never sees what your
  shell exports, so `HIVE_DEFAULT_AGENT`, `ANTHROPIC_API_KEY` or `EDITOR` set in
  a `.zshrc` was invisible to the app. Put them in
  `~/.config/hive/desktop/.env` instead and Hive uses them for its own
  configuration and for every command it runs. Point `environment.file` at
  another path to keep one `settings.yaml` on two machines with a different env
  file on each. `PATH` and `HIVE_DESKTOP_*` are ignored, a variable the launch
  already carries wins, and the file is read once, at startup.
