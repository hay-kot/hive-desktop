---
title: Sign in to GitHub
description: The first-run flow — name a workspace, then authorize Hive with GitHub's device flow.
group: Getting started
order: 1
---

The first launch walks you through three steps: **create a workspace → connect
GitHub → turn on notifications.** Connecting GitHub is where a cold start most
often gets stuck, so here it is in detail.

## Create a workspace

A workspace groups your feeds. Give it a name (the placeholder is
*Frontend Triage*) and click **Create workspace**. It starts empty — connecting
GitHub next is what fills it.

## Connect GitHub

Click **Connect GitHub**. The app shows a short **user code** in large type,
along with:

- **Copy code** — copies the code to your clipboard.
- **Open github.com/login/device ↗** — opens the GitHub authorization page.

On that page, paste the code and approve access. The app sits on
*Waiting for authorization…* and completes on its own once you approve — you
don't come back and click anything.

Hive requests the `repo` and `notifications` scopes so it can read your pull
requests, issues, and notification inbox. Your token is stored in the OS
keychain, never in a config file. If the code expires before you approve it,
start again to get a fresh one.

> **Prefer a token?** Click **Use a token instead** and paste a personal access
> token with `repo` and `notifications` scopes.

## Confirm you're signed in

Onboarding moves straight to the notifications step once GitHub approves — there
is no "signed in as" banner on that screen. To check later, open
**Settings ▸ Integrations ▸ GitHub**; a connected account reads
**Connected as `<your-login>`**.

You *can* skip connecting, but the workspace then has no sources and your feed
stays empty until you connect an account under **Settings ▸ Integrations**.

Next: [turn on notifications](/docs/getting-started/notifications).
