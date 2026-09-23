---
icon: lucide/shield-check
description: See the anonymous adoption data Hive Desktop sends and what it leaves on your device.
---

# Privacy and analytics

Published Hive Desktop builds send one `app_daily_active` event to PostHog per UTC day while the app is running, unless you opt out. This measures active installations. It does not measure foreground time or individual interactions.

Turn **Settings ▸ Analytics ▸ Share daily activity** off to opt out. The change takes effect immediately and stays off across restarts. The same preference is available in `settings.yaml`:

```yaml
analytics:
  enabled: false
```

The event contains:

- a random installation UUID
- the Hive Desktop version and release channel
- the operating system and CPU architecture

The UUID is stored in `<data-dir>/desktop/adoption.json`. It is not based on a user account, device identifier, email address, or machine name. Deleting the file creates a new installation UUID the next time Hive runs.

Hive does not send names, email addresses, repository data, source events, file paths, host names, flow contents, or commands. Events are sent without a PostHog person profile, and PostHog GeoIP enrichment is disabled. The HTTPS request still reaches PostHog from your network address, as any request to a remote service does.

Builds made from source send nothing unless the builder supplies both `HIVE_DESKTOP_POSTHOG_PROJECT_TOKEN` and `HIVE_DESKTOP_POSTHOG_ENDPOINT` at build time.
