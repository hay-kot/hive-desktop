# Shipped builds report anonymous daily installation activity to PostHog

- **Status:** accepted
- **Date:** 2026-09-23

## Context

Adoption needs one comparable count: how many installations run Hive on a given day. The app has no Hive account and does not need product interaction analytics for this count. The existing OpenTelemetry export is user-configured operational telemetry and cannot measure adoption across installations.

A daily active installation still needs a stable identifier so PostHog can count one installation once per day. Names, email addresses, source data, file paths, host names, IP-derived location, and interaction events are not needed.

## Decision

Official builds can embed a PostHog project token and ingestion origin through `HIVE_DESKTOP_POSTHOG_PROJECT_TOKEN` and `HIVE_DESKTOP_POSTHOG_ENDPOINT`. If either value is absent, adoption reporting is off and creates no local state. When the destination exists, `analytics.enabled` defaults on. Settings ▸ Analytics exposes it as **Share daily activity**, and turning it off cancels any in-flight capture and applies without a restart.

An enabled build stores a random installation UUID and the last successfully reported UTC date in `<StateDir>/adoption.json`. It sends `app_daily_active` when the core starts and checks hourly while the process remains open. The event contains the installation UUID, app version, release channel, operating system, and architecture. It sets `$process_person_profile` to `false` and `$geoip_disable` to `true`, and never calls identify. The PostHog request receives the network address as any remote HTTPS service does, but PostHog does not derive GeoIP event properties from it.

The reporter is one App-owned background subsystem. A failed request does not affect startup and remains due for the next hourly check. A successful request records the date locally. Development and mock builds carry no token and send nothing.

## Consequences

- PostHog daily unique users for `app_daily_active` represent daily active installations, not people and not foreground interaction.
- The installation UUID is a persistent pseudonymous identifier. Removing the state file or reinstalling with a fresh data directory creates a new one.
- One small HTTPS request is sent per UTC day while Hive is running and the user has not opted out. A machine that stays open in the background still counts.
- Build and release configuration must provide both PostHog values for adoption reporting to be active.
- No PostHog SDK is added; the reporter uses the capture endpoint directly.
