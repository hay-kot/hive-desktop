# Security policy

## Reporting a vulnerability

Report it privately, through GitHub:

**<https://github.com/hay-kot/hive-desktop/security/advisories/new>**

Only the repository maintainers can read a draft advisory. Please do not open a
public issue, a pull request, or a discussion for anything you think is a
vulnerability -- Hive runs on developer machines with real credentials in the
keychain, so a public report is a working exploit until the fix ships.

Useful things to include, none of them required:

- The version and channel from Settings, About.
- Your OS and version.
- What an attacker needs first: local access, a machine on the same network, a
  crafted webhook payload, a malicious repository the user is subscribed to.
- A proof of concept, or the code path if you only read it.

## What to expect

One person maintains this project.

- A first reply within 7 days.
- An assessment -- confirmed, not a vulnerability, or needs more from you --
  within 30 days.
- Credit in the advisory and the release notes, unless you ask not to be named.

If 14 days pass with no reply at all, open a public issue that says you are
waiting on a security report and nothing else. Keep the details in the advisory.

Fixes go into the next release on the stable channel. Older versions get
nothing back-ported.

## Scope

Hive Desktop is a desktop app that holds credentials and runs commands the user
configures. The parts most worth attacking:

- **Stored credentials.** Account tokens live in the OS keychain
  (`internal/app/credentials/keychain.go`). The GitHub token carries the `repo`
  and `notifications` scopes, so it reads every private repository the user can
  reach.
- **The loopback HTTP API.** The app serves an API on localhost behind a bearer
  token minted for that run (`internal/adapter/httpapi/`). Anything that reaches
  it with the token drives the app.
- **The MCP servers.** `/mcp` and `/mcp/canvas` ride the same loopback surface
  and the same token. They are how an agent reads the inbox and writes canvases.
- **The webhook listener.** It serves `/hooks/<path>` for webhook sources. The
  shared secret is optional per source, so an unauthenticated listener is a
  supported configuration and untrusted payloads are the normal case.
- **Terminal sessions and actions.** The app starts tmux sessions and runs
  actions from `actions.yml`; both inherit the user's shell environment.
- **Flows.** The `function` node runs JavaScript from the user's `flows/`
  directory.

Report anything that crosses one of those boundaries without the user asking for
it.

## Out of scope

- `internal/hivecore/` is vendored from
  [colonyops/hive](https://github.com/colonyops/hive). Report a flaw in that
  code upstream, not here.
- A user who configures an action, a flow, or a webhook to do something harmful
  on their own machine. That is the app working.
- Reports from an automated scanner with no path to exploitation shown.

## Agents

If an LLM writes the report, use ASD-STE100 Simplified Technical English: active
voice, approved-vocabulary words, sentences of 20 words or fewer, no gerunds.
See [CONTRIBUTING.md](CONTRIBUTING.md) > Writing style. The same rule covers the
issue templates, pull request bodies, and commit messages.

One rule on top of the style, because it matters more here than anywhere else:
separate what you ran from what you read. Say which one each claim comes from. A
report that reads a code path and says so is useful. A report that presents the
same reading as a reproduction wastes the response window on a bug that may not
exist.
