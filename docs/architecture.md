# Architecture

How Hive Desktop is structured and how it should grow. This is a standing
reference: read it before adding a subsystem, an entrypoint, or an extension
point, and treat the [rules](#rules-for-every-pr) as review criteria.

Companion documents: [`source-pipeline.md`](source-pipeline.md) describes the
pipeline's runtime behaviour; ADRs in [`decisions/`](decisions/) record
individual choices; this document describes the shape everything fits into.

> **Status: partly built.** The dependency rule, the wrapper idiom, and the
> consumer-defined-interface style hold today, and so does the core's shape:
> the `app`/`adapter` split (`internal/app/` is the core,
> `internal/adapter/wailsui/` holds every Wails service, `desktop/` is
> `main()` plus build info), the `app.App` facade with one service per
> domain, `app.Error` with its `Kind` vocabulary, the typed `app/events` bus,
> and `context.Context` first on every core method.
>
> Several of those are enforced rather than reviewed: `depguard` fails a core
> package that imports Wails or an adapter, `forbidigo` fails
> `application.Get`, `context.Background` or an `emit*` helper outside the
> adapter, `containedctx` fails a stored request context, and
> `mise run check:bindings` fails a service that moved without regenerating
> its bindings.
>
> The flow engine runs in Go: `internal/app/runtime` owns graph execution,
> `runtime/js` implements the `ScriptRuntime` port with goja, and `app.Engine`
> drives it — a runner per enabled flow, reinstalled on a flows change,
> draining the log on every append (ADRs 0010 and 0011). The browser engine is
> gone, and with it the six RPCs that existed only to feed it. Flow execution
> no longer depends on a window being open.
>
> Source connectors are declared: `internal/app/sources` holds a registry of
> `connector.Descriptor`s — type, title, pull/push mode, stability, declared
> capabilities, config schema — and `connector.Factory` constructs instances
> where their dependencies live. The producer reads a capability off the
> instance instead of type-asserting for it, and `flow`'s node registry and
> `runtime`'s behaviour registry both derive their source entries from it, so
> adding a connector is a change to `sources/` alone (ADR 0012).
>
> Credentials are keyed by account: `app/credentials` stores a value per
> `Ref{Provider, Account}` in the OS keychain with a separate index of refs,
> a source node names the account it fetches as, and lookup is generic while
> acquisition stays with the connector. GitHub is one connector among them
> rather than a login — nothing is gated on being connected to it, and
> Settings ▸ Integrations is a projection of the same registry (ADR 0013).
>
> Not yet built: the plugs-managed lifecycle and the HTTP/MCP adapters — see
> [Migration path](#migration-path). New work should move toward this shape
> rather than extending the current one.

## The shape

One headless core, several thin adapters, all in one process.

```
                    ┌──────────────────────────────┐
   Wails UI ───────►│                              │
   HTTP API ───────►│      internal/app (core)     │──► SQLite, GitHub, keychain,
   MCP server ─────►│   no transport, no globals   │    tmux, filesystem
   embedded agent ─►│                              │
                    └──────────────────────────────┘
```

Everything runs inside the desktop binary. The producer, the output worker,
and the flow engine are process singletons; a second process running them
would double-poll sources and re-execute actions. Adapters are therefore
in-process callers of the same `App`, not clients over a wire.

`hive` (the CLI/TUI at `colonyops/hive`) is a **separate, external product**.
This repo vendors its core packages read-only and never extends its CLI. When
this document says "CLI", it means a future surface of *this* binary.

### Named patterns

Each row is a canonical pattern name plus the local rule that specialises it.
The name is there so the shape can be recognised and reused without deriving
it; the rule is there because the name alone never determines the constraint
that actually matters here. **Use these names in code review, commit messages,
and doc comments** — a pattern that is named consistently gets built
consistently.

Provenance is marked where it helps: (GoF) Gang of Four, (DDD)
Domain-Driven Design, (Go) an idiom specific to the language.

**Structure — how the app is divided**

| Pattern | Where it applies | The rule here |
| --- | --- | --- |
| **Ports & Adapters** / Hexagonal | the `app` ↔ `adapter` boundary | Driven ports (core → outside) get an interface defined in `app`. Driving ports (outside → core) get **no interface** — adapters depend on concrete types. See [the Go amendment](#the-go-amendment-to-hexagonal). |
| **Facade** (GoF) — as Application Service | `app.App` | One entry point aggregating per-domain services, so a caller never cherry-picks raw dependencies. Mirrors vendored `hivecore/hive/app.go`: *"Commands and TUI consume App instead of cherry-picking raw dependencies."* |
| **Adapter** (GoF) | `wailsui`, `httpapi`, `mcpsrv` | A bound method builds a request and calls a service. More than ~5 lines of logic means it belongs in `app`. Transport vocabulary — status codes, exit codes, wire encodings — stops here. |
| **Anti-Corruption Layer** (DDD) | the `internal/hivecore` seam | Declare a narrow local interface describing only what we need, let the vendored concrete type satisfy it structurally, convert types at the seam. An upstream signature change then breaks one adapter file rather than the app. The idiom is `hive_adapters.go`. |
| **Bounded Context** (DDD) | `app` vs `internal/hivecore` | Two models that must not merge. `hive` is a separate external product with its own vocabulary; its types stop at the ACL and never appear in an `app` signature. This is also why the vendored code is read-only. |

**Behaviour — how variation is handled**

| Pattern | Where it applies | The rule here |
| --- | --- | --- |
| **Strategy** (GoF) | action executors, event delivery, script runtimes | One implementation per variant behind one interface, selected by a registry lookup — never a `switch` that grows a case per type. For event delivery the strategy is *constructed*, not an enum: `events.Coalesce()` / `events.Buffer(n)`, so "coalesce with a queue size" is unrepresentable rather than merely wrong. |
| **Command** (GoF) | action dispatch | An `output_command` row is a durable, replayable command carrying everything its executor needs. Its `UNIQUE (action_id, key)` index is the only thing preventing an already-run action from re-firing — treat it as load-bearing. |
| **Registry** | the four [extension points](#extension-points) | An explicit map keyed by a type string, declared in one file, with per-type config carrying its own `Validate`. Never `init()` self-registration — `gochecknoinits` is enabled. A registry used for enumeration (MCP tool listing, CLI generation) carries *metadata only*, never dispatch. |
| **Factory Method** (GoF) | node config decoding, connector instances | The registry stores a constructor, not an instance. `func() NodeConfig` must return a **distinct** value per call because the decoder mutates it in place. For connectors, a `Descriptor` (schema, capabilities, stability) is what gets registered; instances are constructed per-use from parsed config. |
| **Observer** (GoF) — typed and payload-carrying | core → adapters | Core events carry payloads and each subscriber declares a delivery policy. The Wails adapter degrades them to wake-up signals; **the core never does**, because an MCP client cannot cheaply "re-read the service" and a streaming consumer needs the delta. |
| **Declared capabilities** | connectors, any pluggable type | A type states what it supports. Never `if s, ok := x.(Backfiller)` — sniffing hides capability from the editor, the docs, and an LLM, all of which need to know before calling. |
| **Unit of Work** (PoEAA) | operations spanning two domains | The transaction is ambient on the `context`, not a parameter threaded through every signature. A store method begins `db := db.Ctx(ctx)` and thereby joins whatever transaction is already open; `WithinTx` **joins** an ambient transaction rather than nesting, because SQLite has none and a second `BEGIN IMMEDIATE` deadlocks against the first. Only the outermost caller commits. |

**Modelling and Go idiom**

| Pattern | Where it applies | The rule here |
| --- | --- | --- |
| **Value Object** (DDD) | `Ref{Provider, Account}`, `Sink` | Immutable, compared by value, self-validating, no identity of its own. A credential reference is a `Ref`, never a bare string. Credential *values* are plain strings — see [Credentials](#credentials). |
| **Consumer-defined interfaces** (Go) | every dependency edge | The interface belongs to the package that *uses* it, not the one that implements it. Keep it to the methods actually called. House style: `pipeline.Appender`, `OutputCommandStore`, `FlowLister`, `flow.Refs`. Never define an interface "for mocking" on the implementor side. |
| **Single declaration, many consumers** | node and action types, later connector config | One Go declaration — schema plus prose — feeds the editor form, the node drawer, and an LLM. A bijection test fails if a registered type has no doc. ADR 0009. This is the pattern every new extension point should extend. |
| **Typed errors, mapped once per adapter** | every boundary | Core returns an error carrying a `Kind`; each adapter maps `Kind` to its own vocabulary exactly once. Nothing anywhere matches on error *text*. |
| **Options struct** (Go) | store and subsystem constructors | `store.DefaultOpenOptions()`, `activity.Options{Emit: …}`. A new optional dependency is a field on the options struct, not a new constructor. |
| **One instance per process** | producer, output worker, flow engine | Constructed once by `App` and injected. Deliberately **not** GoF Singleton: no global access point and no lazy self-construction — the constraint is "exactly one exists", not "anyone can reach it". Two would double-poll sources and re-execute actions. |

### Which pattern governs what

An agent adding a feature should be able to find its shape here without
reading the whole document. Left column is what you are building; right
column is the section that specifies it.

| Building… | Patterns that govern it | Specified in |
| --- | --- | --- |
| A new **node type** | Registry, Factory Method, Single declaration | [Extension points](#extension-points) |
| A new **action type** | Registry, Command, Strategy (the `Executor`) | [Extension points](#extension-points) |
| A new **source connector** | Factory Method (`Descriptor` → instance), Declared capabilities, Value Object (credential `Ref`) | [Source connectors](#source-connectors) |
| A new **script language** | Strategy behind the `ScriptRuntime` port, Registry | [Script nodes](#script-nodes) |
| A new **bound method / RPC** | Facade, Adapter, Typed errors | [Placement rules](#placement-rules), rules 1–4 |
| A new **HTTP, MCP or CLI surface** | Adapter, Ports & Adapters (driving side — no interface) | [The Go amendment](#the-go-amendment-to-hexagonal) |
| A new **event** | Observer — payload in core, degraded to a wake-up in `wailsui` | [Events](#events) |
| A new **background subsystem** | One instance per process, registered with the plugs manager | [Background lifecycle](#background-lifecycle) |
| A new **persisted field** | Config-vs-data boundary; Value Object for anything secret-bearing | [Config versus data](#config-versus-data), [Credentials](#credentials) |
| An operation **spanning two domains** | Unit of Work — `db.Ctx(ctx)` to join the ambient transaction, never a second one | [Config versus data](#config-versus-data) |
| A new **dependency on something outside** | Consumer-defined interface in the package that calls it | [Layers and the dependency rule](#layers-and-the-dependency-rule) |
| Anything touching **vendored code** | Anti-Corruption Layer, Bounded Context — wrap, never edit | [Layers and the dependency rule](#layers-and-the-dependency-rule) |
| A new **outbound HTTP call** | `appkit/httpclient` with composable middleware, not a bespoke client | [Background lifecycle](#background-lifecycle) |

If what you are building is not on this list, it is probably a service method
on `App` — see [Placement rules](#placement-rules).

### The Go amendment to hexagonal

Go's own guidance is that [interfaces belong in the package that *uses*
them](https://go.dev/wiki/CodeReviewComments#interfaces). Applied to the two
port kinds, that splits:

- **Driven ports** (core → outside: stores, HTTP clients, keychain, notifier,
  script runtimes) — interfaces **defined in `app`**, because `app` is the
  consumer. Implementations live in adapters or infrastructure packages and
  satisfy them structurally.
- **Driving ports** (outside → core: Wails, HTTP, MCP) — **no interface**.
  Adapters depend on `*app.App` and the concrete service types directly.

There is no `type FlowService interface` in this codebase. If one appears,
something has gone wrong.

### What we deliberately do not do

- **No command bus / mediator.** Handlers are concrete fields on `App`;
  dispatch is a direct method call so it stays type-safe and traceable. If a
  registry is needed to enumerate operations (MCP tool listing, CLI
  generation), it carries *metadata only* — never dispatch.
- **No `core/` + `adapters/` + `ports/` taxonomy inside the core.** Packages
  under `app/` are named for their domain (`flow`, `inbox`, `ingest`), not
  their layer.
- **No dynamically loaded plugins.** No `.so`, no out-of-process gRPC
  plugins. Extension points are compile-time registries. Revisit only if a
  third party needs to ship a connector.
- **No interfaces "for mocking" on the implementor side.** Define the narrow
  interface where it is consumed; the concrete type satisfies it for free.

## Layers and the dependency rule

```
adapter/*  ──►  app/*  ──►  hivecore/*  (vendored, read-only)
                  │
                  └──►  appkit, stdlib, third-party
```

Dependencies point one way. `app` must never import `adapter`, Wails, or any
transport package. An `app` package that needs something from the outside
declares an interface and takes it as a constructor parameter.

`hivecore` is vendored from `colonyops/hive` at a pinned SHA and regenerated
wholesale by `cmd/vendorhive`. Never edit it here; wrap it. The
established wrapper idiom is in `hive_adapters.go`: declare a narrow local
interface describing only what we need, let the vendored concrete type satisfy
it structurally, and convert types at the seam — so an upstream signature
change breaks compilation at one adapter file rather than across the app.

## Directory structure

```
cmd/                              # build and maintenance CLIs (urfave/cli), not the app
  release/  vendorhive/

desktop/                          # Wails app package — stays `main`, stays here
  main.go                         # bootstrap → build App → mount adapters → run
  buildinfo.go                    # version/commit/channel
  build/  e2e/  frontend/
    frontend/src/pipeline/
      nodes/*/                    # config.ts, editor.vue, index.ts — editor only.
                                  #   Help text is NOT here; it comes from the Go
                                  #   schema package via the @nodedocs alias.
      lib/  fields/               # canvas and form primitives

internal/
  app/                            # THE CORE. No Wails, no transport, no globals.
    app.go                        # App facade
    errors.go                     # Error{Kind, Msg, Err}; Kind enum
    events/                       # typed bus
    flow/                         # flow YAML: parse, validate, save, watch, layout
      docs/                       # per-node-type markdown — read by the node drawer
                                  #   AND by an LLM (ADR 0009)
    runtime/                      # graph engine
      graph.go  run.go            #   index a flow, execute a batch -> CommitBatch
      nodes.go                    #   what each node type does at run time
      filter.go  function.go      #   the two processing node types
      script.go                   # ScriptRuntime / ScriptInstance ports + registry
      js/                         # goja implementation
      testdata/parity/            # the engine's own regression fixtures
    icons/                        # curated feed glyph set — a leaf, shared by
                                  #   flow's feed node and the webhook connector
    sources/                      # connector registry
      registry.go                 #   the map of descriptors, in one file
      connector/                  #   the vocabulary — a leaf, so a connector can
                                  #   name it without importing the registry back
      github/  webhook/  …        #   Descriptor + Config + Factory per connector
    ingest/                       # producer loop, classification, absence, snapshots
      resolver.go                 #   the flow set -> live connector instances
    dispatch/                     # output worker, Dispatcher, executors
    actions/                      # actions.yml catalog, watcher, editable model
      docs/                       # per-action-type markdown
    prompts/                      # Go-owned LLM prompt templates + registry (ADR 0009)
    inbox/                        # item queries, triage, action-item decoding
    credentials/                  # Ref{Provider, Account}, Store, keychain, index
    jobs/  activity/              # observability domains
    settings/                     # settings.yaml, paths, bootstrap pointer file
    notify/                       # Notifier port only
    store/                        # sqlc, migrations, queries

  adapter/                        # driving adapters, all in-process
    wailsui/                      # Wails service structs; the only Wails imports
      events.go                   # bus subscriber → Emit, coalesced to wake-ups
      window.go  tray.go  focus.go  updater.go  notify.go
      e2e/                        # state-reset and smoke middleware
    httpapi/                      # REST + SSE, mounted via ServeHTTP at a Route
    mcpsrv/                       # tools over App; in-memory transport for the agent

  hivecore/                       # vendored, read-only
```

`cmd/` holds developer tooling that ships separately from the app — the
release publisher and the vendor sync. It is not where app entrypoints go;
every user-facing surface is an adapter inside the desktop binary.

### Placement rules

- A Wails service struct lives in `adapter/wailsui/`. No exceptions — every
  bound service belongs to the adapter.
- Anything a CLI, MCP tool, or HTTP handler would need lives in `app/`.
- GUI-only state (window focus, tray, native dialogs, the updater) is adapter
  code and stays there.
- Wire/DTO types shaped by a transport belong to that adapter, not to `app`.

## Extension points

Four registries, all the same shape: an explicit map keyed by a type string,
declared in one file, with per-type config carrying its own `Validate`.

| Extension | Registry | Adding one means |
| --- | --- | --- |
| **Node type** | `app/flow` + `app/runtime` | config struct + `Inputs`/`Outputs`/`Validate` and one line in `flow`'s registry; one line in `runtime`'s behaviour registry saying what it does with a message (relay, sink, or process); `flow/docs/<type>.md`; plus `nodes/<type>/{config.ts,editor.vue,index.ts}` for the editor. A test fails if a type is in one registry and not the other |
| **Action type** | `app/actions` | config struct + `Validate`, one registry line, `actions/docs/<type>.md`, an `Executor`, one dispatcher line, and the editable-catalog branch |
| **Source connector** | `app/sources` | a `Descriptor`, a config struct with `Validate`, and a `Factory` — plus one line in `sources/registry.go` and one in `app`'s factory map. `flow`'s and `runtime`'s registries derive their entries, so neither is touched. Still needs `flow/docs/<type>.md` and a `nodes/<type>/` editor entry until forms are schema-driven |
| **Script runtime** | `app/runtime` | a `ScriptRuntime` implementation and one registry line |

### Documentation is part of the declaration

Per-type prose lives beside the Go schema in `flow/docs/` and `actions/docs/`,
not in the frontend and not in a prompt template. The frontend reads those same
files through the `@nodedocs` Vite alias rather than keeping a copy, and a
registry↔docs bijection test fails if a registered type has no doc. Because
that text is read by both the node drawer and an LLM, it must stay free of
UI-only references like "the row below".

This is the pattern every extension point should follow, and it is the
strongest existing evidence for the direction in this document: one
declaration in Go, consumed across the language boundary, enforced by a test
rather than by discipline. Extend it — a source connector's config schema
should feed the editor form and, later, an MCP tool's input schema from the
same source.

### Source connectors

The connector interface is the one most likely to age badly, so it is
specified rather than left to grow. ADR 0012 records why.

- **Declaration is separate from instance.** `connector.Descriptor` carries
  type name, title, mode, stability level, declared capabilities, and a config
  factory. It is static data with no dependencies — which is what lets the
  registry hold it as package state and lets `flow` derive a node type from it.
  `connector.Factory` constructs instances, and is built in `app.New` where
  the dependencies exist.
- **The framework parses and validates config.** A connector supplies a config
  struct with `json`/`yaml`/`jsonschema` tags plus a `Validate() error` for
  cross-field rules. It never decodes YAML itself. The schema is reflected from
  the struct, so it cannot describe a field the struct lacks; it will feed the
  MCP tool input schema and, later, a schema-rendered editor form.
- **Pull and push are distinct.** GitHub, RSS, Grafana, and PostHog are pull;
  directory watchers and HTTP endpoints are push. The `Mode` on the descriptor
  is what separates them, and the resolver answers `PullInstances` and
  `PushInstances` from one walk — rather than forcing push sources to fake a
  blocking read.
- **Capabilities are declared, not sniffed.** No `if s, ok := src.(Backfiller)`
  — a connector states what it supports, and a test fails when a descriptor
  declares a capability its factory does not wire. Sniffing fails *open*: the
  assertion still compiles, the source still polls, and it silently ingests
  with no classifier and no absence confirmation.
- **Config references credentials, never embeds them.** See below.
- **Never mirror the upstream API's shape in connector config.** Provider
  vocabulary leaking into the flow schema is permanent.

Connector type strings are namespaced — `sources.github`, `sources.webhook` —
so connectors group and sort together everywhere node types are enumerated:
the palette, `flow/docs/`, and the flows prompt. The other node types are not
namespaced by category, because a `sinks.` or `process.` prefix would declare
what [`CategoryOf`](#documentation-is-part-of-the-declaration) *derives* from
port counts, and a declared category can disagree with what the graph
validator enforces.

The registry is a package-level map in one file, so `sources` imports the
connector packages and they must not import it back. The vocabulary both sides
name therefore lives in a leaf, `sources/connector`.

## Cross-cutting conventions

### Context

Every core method takes `context.Context` as its first parameter. Adapters
pass the caller's context through. `context.Background()` inside a service
method is a bug — it makes CLI cancellation and HTTP deadlines impossible.

### Errors

Core returns a typed error carrying a `Kind`; each adapter maps `Kind` to its
own vocabulary exactly once — HTTP status, MCP error, CLI exit code, Wails
message. No HTTP status codes or transport concepts in `app/`. Nothing should
ever need to match on error *text*.

### Events

The core publishes **payload-carrying typed events** to `app/events`.
Subscribers declare a delivery policy: coalesce-and-drop for a GUI that only
needs the latest state, buffered/blocking for a consumer that must see every
message.

The Wails adapter degrades these into the existing wake-up signals
(`log:appended`, `flows:updated`, …) where the frontend re-reads on receipt.
That contract is good for a GUI and stays. It must not be pushed down into the
core: an MCP client cannot cheaply "re-read the service", and a streaming CLI
or SSE consumer needs the delta.

`application.Get()` must not appear outside `adapter/wailsui/`.

### Credentials

Secrets live in `app/credentials`, keyed by `Ref{Provider, Account}` and
stored in the OS keychain. Two constraints drive the design:

- **Keychains do not enumerate.** `List()` needs a separate index of refs
  (`<StateDir>/credentials.json`); only the values live in the keychain. The
  keychain is the truth and the index is a cache: a ref present in the index
  but absent from the keychain reads as `ErrNotFound` and is pruned, so a
  divergence can never surface as a stuck "Connected" badge.
- **Config holds refs, never tokens.** `flows/*.yaml` is explicitly
  dotfiles-managed, so a token in a node's config is a token in a git repo. A
  source node carries `credential: grafana/prod`, resolved at construction —
  and re-read on every use, so connecting, rotating or disconnecting an
  account takes effect on the next fetch with nothing to invalidate.

**Values are plain strings.** A redacting wrapper earns its place when secrets
are loaded from config and then flow through structs that get marshalled and
logged; "config holds refs, never tokens" means that path does not exist here.
A value's whole life is keychain → connector factory → provider client, and
none of those marshal it.

**Lookup is generic; only acquisition is provider-specific.** `Resolve`,
`Bind`, `ListProvider` and `EnvOverrideName` know no provider — the headless
override is derived from the provider name, so `HIVE_GITHUB_TOKEN` and
`HIVE_GRAFANA_TOKEN` both come for free. Acquisition belongs to the connector:
GitHub's device flow lives in `app/sources/github`, while Grafana is a
secret-marked config field with no state machine. `Connection` is declared by
the connector that implements it, not by `app` — a connector needing no state
machine declares none.

**Multi-account is the model, not an extension of it.** One fetcher per
account: a `feed.LiveProvider` holds one account's response cache,
conditional-request state and rate-limit cooldown, so two accounts sharing one
would serve each other's items and stall each other's fetches.

**GitHub is a connector, not a login.** Nothing in the app is gated on being
connected to it. First run is create workspace → connect GitHub → feed: the
workspace is the one thing that exists without a credential, so it goes first,
and connecting is what seeds its starter graph. Bypassing that step is possible
past a warning, and lands on a feed whose empty state points at Settings ▸
Integrations — itself a projection of the connector registry, joined to what
the credential store holds. ADR 0013 records why this is a new store rather
than an extension of the vendored single-slot one.

### Config versus data

User-editable config (`flows/`, `actions.yml`, `settings.yaml`) lives under
`$XDG_CONFIG_HOME/hive/desktop/` so it can be dotfiles-managed. App-local
state (SQLite: items, triage, offsets, queued commands) lives under the data
dir. Respect the boundary when adding persistence.

Two databases remain separate on purpose: `hive.db` is shared with the
external `hive` CLI, and `desktop-pipeline.db` isolates desktop write traffic
from it.

### Background lifecycle

Long-running subsystems — producer, output worker, retention, watchers, the
event bus, the webhook listener, the HTTP and MCP servers — are registered
with a single `appkit/plugs` manager rather than each hand-rolling
`Start`/`Stop`, a `stopOnce`, and a teardown branch in `main`. That gives
uniform panic capture, retry with backoff, signal handling, and one graceful
shutdown path.

Other `appkit` packages with a clear home here: `httpclient` (context-first
client with composable middleware — the fetch layer connectors need, which
does not exist today) and `mapx`.

**`httpclient` cannot reach the one fetch path that exists.** GitHub's requests
go through the vendored `hivecore/github.Client`, whose transport is a concrete
`*http.Client` field with only a `WithHTTPClient(*http.Client)` option;
`httpclient.Client` wraps an `*http.Client` rather than being one, so it cannot
be substituted without landing a change in `colonyops/hive` and re-vendoring
(rule 9). The app's only other outbound HTTP is the updater and `cmd/release`,
neither a connector fetch path. Adopt it when a connector that owns its own
HTTP lands, or when the upstream client takes an injectable `Doer`.

## Execution model

The flow engine runs **in Go**, in-process. Source polling, graph routing,
script evaluation, and action execution all happen in the core; the frontend
owns the editor, not the runtime.

This replaces a split model in which Go ingested and executed while the
browser routed. That split made the desktop window a hard dependency of flow
execution and put the correctness-critical parts — topological order,
fan-out, offset advancement, commit atomicity — out of reach of any headless
surface. Consolidating in Go is what makes `dry-run` available identically to
the editor preview, a CLI, and an MCP tool.

`Runner.Run` returns the `CommitBatch` a batch of messages is worth and
**does not commit it**. Reading the log and applying the batch belong to the
caller, which is what makes a dry-run and a live tick one code path rather
than two implementations of the same semantics.

Two registries describe a node type between them: `flow`'s says how it is
configured, `runtime`'s says what it does when a message arrives. Neither the
router nor the executor branches on a type string.

`app.Engine` is what drives it. It is level-triggered: `Wake` and `Reload` set
a latch rather than queueing, so a signal arriving mid-pass is serviced by the
next pass instead of being dropped or piling up. Installation is synchronous in
`Start`, before anything that can append to the log is running, so no append
can arrive with no runner to route it.

A flow that cannot be built keeps its predecessor in service and reports the
failure to the activity log — taking a working flow offline for an authoring
mistake would lose messages the last-known-good version handles.

`internal/app/runtime/testdata/parity/*.json` are the engine's fixtures: a
flow, a batch, and the exact commit it is worth. They began as the proof the
port off the browser engine was faithful, and a change to routing, sink tagging
or accounting still belongs in one. ADR 0011.

### Script nodes

`function` nodes execute user-authored JavaScript through **goja** (pure Go;
a cgo engine would break the CGO-free server build). The runtime sits behind
the `ScriptRuntime` port so a second language can be added without touching
the engine.

Contract:

- **`on_message` only.** No lifecycle hooks — the function node has no I/O, so
  `on_stop` could only mutate state that is about to be discarded, and
  `on_start` is better expressed as lazy init (`state.counts ??= {}`). This
  also removes the pretence that `msg` may be undefined.
- **Port-indexed outputs.** The port's canonical return is `[][]Msg`, indexed
  by output port. Each language adapter resolves its own ambiguity (JS's
  overloaded array shapes, for instance) internally; the engine never sees an
  ambiguous value.
- **Normalized errors.** Compile, runtime, and timeout failures map to one
  `ScriptError` type with line/column, so the editor renders any language's
  diagnostics identically. Syntax checking is a core concern, exposed to the
  editor.
- **Cooperative timeouts.** `context.AfterFunc` plus `vm.Interrupt` replaces
  the browser's `worker.terminate()`. Evaluation runs on a goroutine the
  runtime is willing to abandon, so a script that outlives its interrupt does
  not stop its flow; the abandoned goroutine holds a slot in a process-wide
  `ScriptPool` until it returns, so the damage is a fixed budget rather than a
  hang. A node that times out is respawned with fresh state. This is a known,
  accepted limitation.

JSON fidelity is the reason for JS over Lua, and it is why values cross the
boundary through the VM's own `JSON.parse`/`JSON.stringify` rather than
`vm.ToValue`: a Go slice handed to goja answers `false` to `Array.isArray`,
whereas parsed JSON is a real JavaScript value and round-trips `null`, empty
arrays, empty objects, and key order without bridge helpers or sentinels.

The message envelope is a fixed set of fields. A property attached to `msg`
itself is not carried downstream — that envelope is also what an HTTP or MCP
surface serialises — so per-message data belongs in the opaque `msg.Payload`.
ADR 0010.

## Rules for every PR

1. **No logic in `desktop/` or `adapter/wailsui/`.** A bound method builds a
   request and calls a service. More than ~5 lines means it belongs in `app/`.
2. **No `application.Get()` outside `adapter/wailsui/`.**
3. **`context.Context` first, always.** No `context.Background()` in a service
   method.
4. **Typed errors.** Return an `app` error with a `Kind`; never let callers
   match on message text.
5. **Events carry payloads in the core.** Degrade to wake-up signals in the
   Wails adapter, never earlier. No new package-level `emit*` functions.
6. **No transport encoding in core signatures.** Precision workarounds like
   int64-as-string are adapter concerns.
7. **New extension types register in the registry** and declare a config
   schema. No per-type branching in shared code.
8. **New background work registers with the plugs manager.** No bespoke
   `Start`/`Stop` pairs and no new teardown branches in `main`.
9. **Never edit `internal/hivecore/`.** Land the change upstream and re-vendor.

## Migration path

The target is reached in this order; each step is independently shippable.

1. **Boundary rename** — `internal/desktop/*` → `internal/app/*`; mechanical,
   no behaviour change, cheapest now and more expensive with every PR.
   **Done.**
2. **Core skeleton** — `App` facade, typed errors, event bus. Move the
   orchestration currently stranded in `package main` (`InvokeAction`, the
   action usage checker's raw SQL, poll-interval validation). **Done.**
3. **Go flow engine + goja**, with parity tests against the TypeScript engine
   before cutover. The largest step. **Done.** ADRs 0010 and 0011. The shared
   fixtures both engines had to satisfy live on in
   `internal/app/runtime/testdata/parity/` as the engine's own regression
   suite.
4. **Delete the frontend engine** — `engine/`, `driver.ts`, the runtime
   pump — and collapse the six frontend-only RPCs into internal calls.
   **Done.** `app.Engine` drives the runners and owns the replay protocol;
   `PipelineService` no longer carries `ReadFrom`, `Commit`,
   `EventLogTailOffset`, `ActivateReplay`, `ListReplaySourceSnapshots` or
   `ListUnarchivedInboxItems`, and the int64-as-string offset encoding they
   needed went with them.
5. **Source registry** — **Done.** ADR 0012. `connector.Descriptor` declares a
   connector and `connector.Factory` constructs it; capabilities are read off
   the instance rather than type-asserted; `flow`'s and `runtime`'s registries
   derive their source entries. Node types are namespaced (`sources.github`,
   `sources.webhook`), which breaks the `type:` discriminator in any existing
   `flows/*.yaml`.
6. **Credentials** — **Done.** ADR 0013. `Ref{Provider, Account}`, the
   keychain-backed store and its ref index, one fetcher per account, and
   GitHub demoted from a login to a connector. A source node's `credential:`
   is required, which breaks any existing `flows/*.yaml` a second time.
   First run creates the workspace before it offers to connect anything, and
   `flow` no longer names a connector: `FlowStore.Create` takes its starter
   graph from its caller.
7. **Adapters** — HTTP and MCP mounted in-process; plugs for lifecycle.

### Data that must survive

Breaking changes to schema and config format are acceptable. Two things are
not rebuildable and must be carried across any migration:

- `inbox_item.unread` / `archived_at` / `archived_actor` / `archived_reason` —
  triage decisions, not derivable from any source.
- `output_command`'s unique `(action_id, key)` index — the only thing
  preventing an already-run action from re-firing.

Everything else (`event_log`, `feed_membership_claim`, `node_run`,
`source_head`, `consumer_offset`, `activity_event`, `job`) is derived or
replayable. Migrations are not squashed, because a squashed `0001` would not
match existing `schema_migrations` rows.

## Open questions

These are deliberately unresolved; revisit when the relevant work starts.

- **Command placement** — per-domain service methods with request structs
  (current plan, matching `hivecore/hive/app.go`) versus a flat
  `app/command` + `app/query` package that gives MCP and CLI generation one
  place to enumerate.
- **`adapter/` as a grouping directory** versus flat `internal/wailsui`,
  `internal/httpapi`, `internal/mcpsrv`.
- **Schema-driven editor forms** — a connector's config schema is reflected
  and available, but `nodes/<type>/editor.vue` is still hand-written per type,
  as is its duplicated UX-only `validate()`. Rendering the form from the
  schema is what would make "one declaration in Go" true across the language
  boundary; the forms are hand-tuned (conditional fields, glob lists, a code
  editor), so a generic renderer has to earn its place.
- **A `runtime:` field on function nodes** — deferred until a second script
  language exists. The port and registry are in place; the config field is
  not.
