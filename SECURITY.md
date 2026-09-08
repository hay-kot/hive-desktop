# Security Policy

## Supported Versions

Security fixes ship in the next release. Earlier releases are not patched.

| Version          | Supported          |
| ---------------- | ------------------ |
| Latest release   | :white_check_mark: |
| Earlier releases | :x:                |

## Reporting a Vulnerability

Report vulnerabilities privately through GitHub:

https://github.com/hay-kot/hive-desktop/security/advisories/new

Draft advisories are visible only to the repository maintainers. Please do not
open a public issue, pull request, or discussion for a suspected vulnerability.
Hive Desktop stores real credentials on developer machines, and a public report
can be used against every installed copy before a fix is available.

A report is easier to act on when it includes:

- The affected version and channel, shown under Settings > About.
- Your operating system and version.
- The preconditions an attacker needs, such as local access, a machine on the
  same network, a crafted webhook payload, or a malicious repository.
- Steps to reproduce, a proof of concept, or the code path you believe is
  affected. Please say whether you reproduced the issue or found it by reading
  the code.

You can expect:

- An acknowledgement within 7 days.
- An assessment within 30 days: confirmed, not a vulnerability, or a request
  for more information.
- Credit in the advisory and the release notes, unless you prefer not to be
  named.

If you have not heard back after 14 days, open a public issue stating that you
are waiting on a response to a security report. Do not include any details of
the report in that issue.

## Scope

Hive Desktop is a desktop application that stores credentials and runs commands
configured by the user. The areas of most interest are:

- **Stored credentials.** Account tokens are stored in the OS keychain. The
  GitHub token carries the `repo` and `notifications` scopes.
- **The loopback HTTP API and MCP servers.** The app serves an HTTP API and two
  MCP endpoints on localhost, protected by a bearer token generated for each
  run.
- **The webhook listener.** The app accepts webhook deliveries on
  `/hooks/<path>`. Shared secrets are optional per source, so an unauthenticated
  listener receiving untrusted payloads is a supported configuration.
- **Terminal sessions, actions, and flows.** The app starts tmux sessions, runs
  commands from `actions.yml`, and runs user-supplied JavaScript from `function`
  nodes in flows. All of these inherit the user's environment.

Any way to cross one of these boundaries without the user's action is in scope.

### Out of scope

- Code under `internal/hivecore/` is vendored from
  [colonyops/hive](https://github.com/colonyops/hive). Report issues in that
  code upstream.
- Actions, flows, or webhooks that the user configured to run on their own
  machine.
- Automated scanner output without a demonstrated path to exploitation.
