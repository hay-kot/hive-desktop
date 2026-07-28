---
title: Report a problem
description: Send a diagnostic bundle from inside the app, or grab the log file by hand.
group: Help
order: 0
---

## In-app reporting

The app has a built-in reporter: open **Settings ▸ System** and click
**Report a problem**. It bundles build and system info, a trimmed log tail, and
a **secret-scrubbed** config snapshot, then uploads it — you choose what to
attach.

> **During the beta**, in-app reporting only works if your build had a report
> token configured at release time. If the button is disabled, use the manual
> fallback below.

## Grab the log by hand

The **Diagnostics** section of that same **System** page has **Open** and
**Reveal** buttons for the log file. Or find it directly — on macOS and Linux
alike:

```
~/.local/share/hive/desktop/desktop.log
```

Attach that to your bug report. If the bug looks data-related, the pipeline
database (deliberately excluded from automated reports) sits next to it:

```
~/.local/share/hive/desktop/desktop-pipeline.db
```

## What's in a report

An uploaded report includes build and system details, a bounded log tail, and
your config with secrets removed — tokens, webhook secrets, and action
environment values are stripped before anything leaves your machine. The
pipeline database is never included automatically.
