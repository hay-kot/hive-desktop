---
title: Updates & channels
description: How Hive auto-updates, and how to pick a release channel.
group: Help
order: 2
---

## Auto-update

Hive updates itself. It polls its release channel, and when a newer version is
published it downloads and verifies the build for you — no manual download step.
A build tracks the channel it was released on.

## Channels

There are three channels, from most to least stable:

- **stable** — the default.
- **beta** — earlier access to release candidates.
- **dev** — the newest builds, least baked.

To pin a channel, set it in `settings.yaml`:

```yaml
updates:
  channel: beta # stable | beta | dev
```

Each channel always points at its own latest build, so switching channels moves
you to that channel's current version.
