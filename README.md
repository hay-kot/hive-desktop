# Hive Desktop

Hive pulls the work that wants your attention -- pull requests, issues, review
requests, notifications, firing alerts, and anything that can POST a webhook --
into one local queue. Flows you own filter it, route it into feeds, hand an item
to a coding agent, or fire a command. You get one ping per real change instead
of a dozen browser tabs.

Everything runs on your machine. Triaging in Hive never writes back to GitHub,
and the config is plain YAML you can keep in your dotfiles.

macOS today. Linux is wired up in the installer and waiting on published builds.

## Install

```sh
curl -fsSL https://hivedesktop.com/install.sh | bash
```

The script detects your OS and CPU, resolves the newest build from the same
channel manifest the in-app updater reads, verifies its SHA-256 before touching
disk, installs the app, and symlinks `hive` onto your PATH. Drop the `| bash` to
read it first. [hivedesktop.com/install](https://hivedesktop.com/install) has the
same command with the per-platform notes.

## What it does

- **More than GitHub.** Point a source at any GitHub search or notification
  inbox, across as many accounts as you have. Pair it with Grafana alerts, a
  PromQL expression, or anything that can POST JSON to a local endpoint.
- **Rules that are programs.** Wire sources through filters and functions on a
  canvas. Filters are declarative and their reject branch is wireable. Functions
  are your own JavaScript with durable per-node memory, so one alert can fan out
  into one tracked item per firing entity.
- **One ping per real change.** Re-polling the same PR never re-fires. A cooldown
  floors how often one item can reach you, stale pings are dropped rather than
  replayed as a burst, and a restart recomputes every feed without notifying
  twice.
- **Your coding agent writes the rules.** Hive renders skill files out of its own
  node registry into `~/.claude`, `~/.codex`, `~/.pi` and `~/.agents` and keeps
  them in step. Describe what you want surfaced and your agent writes the flow.
- **Triage becomes action.** An item can launch a coding-agent session from a
  prompt template, run a shell command, publish a message for another session, or
  render to your clipboard.
- **Config is a file you own.** Flows and actions are YAML in your config
  directory. Hive validates on save and reloads live; a file that fails to build
  keeps its last good version in service.

Two further surfaces ship switched off while they settle: **Code**, a tmux-backed
terminal for the sessions your feeds launch, and **Agents**, named workspaces
that generate the config your coding agent reads.

## Documentation

- [hivedesktop.com/docs](https://hivedesktop.com/docs) -- using the app: first
  run, flows, sources and webhooks, actions, agent workspaces, terminal mode,
  the `settings.yaml` reference, keyboard shortcuts, troubleshooting, and
  building from source. Also served as [llms.txt](https://hivedesktop.com/llms.txt)
  for an agent to read. Source in [`web/src/content/docs/`](web/src/content/docs/).
- [`docs/architecture.md`](docs/architecture.md) -- how the app is structured and
  how it should grow. Read this before adding a subsystem or an extension point.
- [`docs/source-pipeline.md`](docs/source-pipeline.md) -- the pipeline at runtime:
  ingestion, flows, membership replay, retention, actions.
- [`docs/decisions/`](docs/decisions/) -- architecture decision records.
- [`docs/development.md`](docs/development.md) -- build it from source.
- [`docs/distribution.md`](docs/distribution.md) -- release and distribution infra.

## Contributing

[`docs/development.md`](docs/development.md) gets you building.
[`CONTRIBUTING.md`](CONTRIBUTING.md) covers the writing style commit messages and
pull requests are held to.

Bugs and ideas go to [issues](https://github.com/hay-kot/hive-desktop/issues).

## License

MIT. See [`LICENSE`](LICENSE).
