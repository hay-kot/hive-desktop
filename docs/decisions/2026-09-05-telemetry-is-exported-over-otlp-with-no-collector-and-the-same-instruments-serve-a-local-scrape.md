# Telemetry is exported over OTLP with no collector, and the same instruments serve a local scrape

- **Status:** accepted
- **Date:** 2026-09-05

## Context

The app observes itself in four disconnected ways: zerolog to a file, pprof
behind `development.pprof` (ADR pprof-debug-endpoint), UI spans in `perf.jsonl`
(ADR ui-performance-spans-are-recorded-to-jsonl), and a `tmuxcc.MetricsSink`
port that has only ever been handed `NopMetrics`. None of them leaves the
machine, none of them can be queried, and none of them can be compared across
two builds. The question this answers is how the app's own metrics, logs and
traces reach a backend where real usage can be read back.

Running a collector alongside a desktop app was rejected outright: it is a
second process to install, supervise and explain, for one producer.

Binary size was the argument against the OTel SDK, so it was measured rather
than assumed. On this app with production flags (39.0 MB baseline) the three
signals together cost **4.52 MiB (+12.2%)** and 73 modules. The shape of the
cost is what decided the scope: in isolation the *first* signal costs about
17 MB and each additional one about 1 MB, because nearly all of it is one
shared OTLP/protobuf/grpc base — grpc is linked even by the HTTP-only
exporters. Staging one signal now and the others later would therefore have
paid the entire bill for a third of the value.

The Prometheus reader was measured the same way: **244 KiB** on top of the
OTLP build, because `client_golang` overlaps almost entirely with what the
exporters already pulled in.

## Decision

1. **OTLP/HTTP with gzip, straight to the endpoint.** `telemetry.endpoint` is
   the signal-less base and the per-signal paths are appended;
   `telemetry.instance_id` is the basic-auth username. No collector, no agent,
   one credential for all three signals. Prometheus remote write would have
   carried metrics alone and left traces and logs needing a second path.

2. **One MeterProvider, two readers.** `telemetry.enabled` adds a periodic
   reader that pushes; `development.metrics.enabled` adds a Prometheus reader
   and mounts `/metrics` on the shared loopback server the way pprof does. The
   gates are independent, so a local scrape answers with no account configured
   and export runs with nothing mounted. An instrument is declared once and
   both readers collect it, which is the point: a PromQL expression written
   against `curl localhost:PORT/metrics` is the same expression run against
   the backend. With both gates off, `New` returns the no-op object `Off`
   returns — no provider, no exporter goroutine, no nil checks at call sites,
   following `perf.Off()`'s precedent.

3. **Four resource attributes, chosen for what survives as a queryable
   dimension.** `service.name`, `service.instance.id`,
   `deployment.environment.name` and `service.version`. A backend keeps only
   some resource attributes as dimensions and files the rest away — Prometheus
   puts the rest in `target_info`, Loki in structured metadata — and
   `service.version` is promoted by Prometheus but **not** by Loki. That
   asymmetry is why the release channel, not the version, is the primary
   separator: it is the only one of the two that selects a log stream.

   `deployment.environment.name` comes from the build's own version string. A
   published build reports its release channel, and anything else — a plain
   `go build`, a dev-task binary, a pseudo-version — reports `source`, because
   `VERSION_LDFLAGS` is applied only on the production branch of the platform
   Taskfiles. A working tree's telemetry therefore cannot land in the same
   series as a release's without anyone configuring that.

4. **The log bridge is a zerolog writer arm, not a `zerolog.Hook`.** A Hook is
   handed only the level and the message; the accumulated fields are not
   readable from a `*zerolog.Event`. Every arm of a `MultiLevelWriter`, by
   contrast, receives the encoded JSON event — which is exactly what the two
   existing `ConsoleWriter` arms parse in order to pretty-print. `NewLogger`
   therefore takes extra writers, and the bridge is one. It never returns an
   error and never fails a write: it is a tap, and an error here would be an
   error about an error.

5. **The token is not a setting.** `settings.yaml` carries the endpoint and
   the instance id, which name a destination and are safe to commit. The token
   is read from `HIVE_GRAFANACLOUD_TOKEN`, the name
   `credentials.EnvOverrideName` derives for the `grafanacloud` provider — so
   the variable read today is the one a stored credential would use, and a
   keychain-backed credential can be added later without renaming anything.

6. **The endpoint is validated for https, deliberately not for loopback.**
   `development.github.api_base` is pinned to loopback precisely so a persisted
   setting cannot aim the app at a remote host. This one is remote by
   definition, so the check that is left to make is that the credential does
   not cross the network in the clear.

## Consequences

- The shipped binary grows 4.52 MiB and `go.mod` gains 73 modules, including
  grpc, which nothing in this app calls. That is the price of the OTLP
  exporters and it is paid once for all three signals.
- Export failures are the SDK's default error handler, which writes to stderr.
  Routing them through zerolog would close a loop — an export error logged
  through the bridge that failed to export — so it is deliberately left alone.
- `otlploghttp` is experimental. The Go log API and SDK reached RC in
  v1.47.0-rc.1 but the exporters were left outside that RC's stability scope,
  so the log path is the one to expect churn from on an upgrade.
- The MVP's whole metric surface is Go runtime metrics and its whole trace is
  `app.startup` with a child per startup phase. `tmuxcc.MetricsSink` is
  deliberately left on `NopMetrics`: it is the consumer-defined interface that
  keeps the SDK out of `tmuxcc`, and giving it a real implementation is the
  next change, not this one.
- `internal/app/perf` and its JSONL file are untouched and still the only way
  to read UI spans without a backend. Whether they are replaced by a
  JSONL `SpanExporter` beside the OTLP one, or kept as they are, is a separate
  decision — this one does not make it.
