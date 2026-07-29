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
> package that imports Wails or an adapter, and a second `depguard` rule fails
> one that imports `internal/hivecore` outside a narrow, commented seam
> allowlist (`app.go`, `dispatch/hive_adapters.go`, `store/dbext.go`, and the
> tests that exercise them); `forbidigo` fails `application.Get`,
> `context.Background` or an `emit*` helper outside the adapter, `containedctx`
> fails a stored request context, and `mise run check:bindings` fails a
> service that moved without regenerating its bindings.
>
> The flow engine runs in Go: `internal/app/runtime` owns graph execution,
> `runtime/js` implements the `ScriptRuntime` port with goja, and
> `runtime.Engine` — constructed by and driven from `App` — runs it: a runner
> per enabled flow, reinstalled on a flows change, draining the log on every
> append (ADRs 0010 and 0011). The browser engine is gone, and with it the six
> RPCs that existed only to feed it. Flow execution no longer depends on a
> window being open.
>
> Source connectors are declared: `internal/app/sources` holds a registry of
> `connector.Descriptor`s — type, title, pull/push mode, stability, declared
> capabilities, config schema — and `connector.Factory` constructs instances
> where their dependencies live. The producer reads a capability off the
> instance instead of type-asserting for it, and `flow`'s node registry and
> `runtime`'s behaviour registry both derive their source entries from it, so
> adding a connector is a change to `sources/` alone (ADR 0012). GitHub's
> connector owns its HTTP client end to end (`sources/github/ghclient`) rather
> than routing through the vendored `hivecore/github` package, and nothing
> outside `internal/hivecore` imports that package anymore (ADR 0015).
>
> Credentials are keyed by account: `app/credentials` stores a value per
> `Ref{Provider, Account}` in the OS keychain with a separate index of refs,
> a source node names the account it fetches as, and lookup is generic while
> acquisition stays with the connector. GitHub is one connector among them
> rather than a login — nothing is gated on being connected to it, and
> Settings ▸ Integrations is a projection of the same registry (ADR 0013).
>
> Desktop configuration is one nested typed schema: startup resolves safe
> defaults, strict YAML and `HIVE_DESKTOP_*` overrides once, then injects the
> resulting settings and immutable path snapshot. Development state is local to
> each worktree under `.hive-desktop/` (ADR 0014).
>
> Partly built: the first HTTP adapter exists —
> `internal/adapter/httpapi` is an agent-facing control surface over `app.App`
> (read, reload, and mutations like setting a profile avatar), mounted onto a
> single loopback `http` server (on by default) that also hosts the webhook
> listener, rather than owning one (ADR 0021). It is self-describing: one
> operations table registers the routes and serves a `GET /api` index plus a
> generated, schema-validated `GET /api/openapi.json` (ADR 0027). It grows toward
> full agentic control, with the same core methods and reflected schemas later
> exposed as MCP tools. The full REST + SSE product surface and the MCP adapter
> are still absent.
>
> Not yet built: the plugs-managed lifecycle (attempted; blocked on appkit —
> see [Background lifecycle](#background-lifecycle)) and the MCP adapter — see
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
| **Error chain** (httpkit `errchain`) | every HTTP surface: `httpapi`, devserver control | Handlers are `func(w, r) error` behind one `web/mid.Errors` middleware that maps error types to responses exactly once — no handler writes a status inline. Input enters only through `web/extractors` (`Body`/`Query` decode + the struct's criterio `Validate`). Per-resource `ctrl_*.go` files, routes registered in one place. See ADR 0022. |
| **Anti-Corruption Layer** (DDD) | the `internal/hivecore` seam | Declare a narrow local interface describing only what we need, let the vendored concrete type satisfy it structurally, convert types at the seam. An upstream signature change then breaks one adapter file rather than the app. The idiom is `hive_adapters.go`. |
| **Bounded Context** (DDD) | `app` vs `internal/hivecore` | Two models that must not merge. `hive` is a separate external product with its own vocabulary; its types stop at the ACL and never appear in an `app` signature. This is also why the vendored code is read-only. |

**Behaviour — how variation is handled**

| Pattern | Where it applies | The rule here |
| --- | --- | --- |
| **Strategy** (GoF) | action executors, event delivery, script runtimes | One implementation per variant behind one interface, selected by a registry lookup — never a `switch` that grows a case per type. For event delivery the strategy is *constructed*, not an enum: `events.Coalesce()` / `events.Buffer(n)`, so "coalesce with a queue size" is unrepresentable rather than merely wrong. |
| **Command** (GoF) | action dispatch | An `output_command` row is a durable, replayable command carrying everything its executor needs. Its `UNIQUE (action_id, key)` index is the only thing preventing an already-run action from re-firing — treat it as load-bearing. |
| **Registry** | the [extension points](#extension-points) | An explicit map keyed by a type string, declared in one file, with per-type config carrying its own `Validate`. Never `init()` self-registration — `gochecknoinits` is enabled. A registry used for enumeration (MCP tool listing, CLI generation) carries *metadata only*, never dispatch. |
| **Factory Method** (GoF) | node config decoding, connector instances | The registry stores a constructor, not an instance. `func() NodeConfig` must return a **distinct** value per call because the decoder mutates it in place. For connectors, a `Descriptor` (schema, capabilities, stability) is what gets registered; instances are constructed per-use from parsed config. |
| **Observer** (GoF) — typed and payload-carrying | core → adapters | Core events carry payloads and each subscriber declares a delivery policy. The Wails adapter degrades them to wake-up signals; **the core never does**, because an MCP client cannot cheaply "re-read the service" and a streaming consumer needs the delta. |
| **Declared capabilities** | connectors, any pluggable type | A type states what it supports. Never `if s, ok := x.(Backfiller)` — sniffing hides capability from the editor, the docs, and an LLM, all of which need to know before calling. |
| **Unit of Work** (PoEAA) | operations spanning two domains | The transaction is ambient on the `context`, not a parameter threaded through every signature. A store method begins `db := db.Ctx(ctx)` and thereby joins whatever transaction is already open; `WithinTx` **joins** an ambient transaction rather than nesting, because SQLite has none and a second `BEGIN IMMEDIATE` deadlocks against the first. Only the outermost caller commits. |

**Modelling and Go idiom**

| Pattern | Where it applies | The rule here |
| --- | --- | --- |
| **Value Object** (DDD) | `Ref{Provider, Account}`, `Sink` | Immutable, compared by value, self-validating, no identity of its own. A credential reference is a `Ref`, never a bare string. Credential *values* are plain strings — see [Credentials](#credentials). |
| **Consumer-defined interfaces** (Go) | every dependency edge | The interface belongs to the package that *uses* it, not the one that implements it. Keep it to the methods actually called. House style: `ingest.Appender`, `OutputCommandStore`, `FlowLister`, `flow.Refs`. Never define an interface "for mocking" on the implementor side. |
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
| A new **HTTP endpoint** | Error chain — `errchain` handler, `web/extractors` input, `ctrl_*.go` + routes in one place | ADR 0022 |
| A new **event** | Observer — payload in core, degraded to a wake-up in `wailsui` | [Events](#events) |
| A new **background subsystem** | One instance per process, App-owned lifecycle (plugs once unblocked) | [Background lifecycle](#background-lifecycle) |
| A new **persisted field** | Config-vs-data boundary; Value Object for anything secret-bearing | [Config versus data](#config-versus-data), [Credentials](#credentials) |
| An operation **spanning two domains** | Unit of Work — `db.Ctx(ctx)` to join the ambient transaction, never a second one | [Config versus data](#config-versus-data) |
| A new **dependency on something outside** | Consumer-defined interface in the package that calls it | [Layers and the dependency rule](#layers-and-the-dependency-rule) |
| Anything touching **vendored code** | Anti-Corruption Layer, Bounded Context — wrap, never edit | [Layers and the dependency rule](#layers-and-the-dependency-rule) |
| A new **outbound HTTP call from a source** | `sources/sourcehttp` over `appkit/httpclient` — never a bespoke client | [Source HTTP](#source-http) |
| A **breaking config schema change** | Forward-only YAML migration runner (per-file `version:`, comment-not-preserving rewrite, backup under StateDir) | [Config versus data](#config-versus-data), ADR 0032 |

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
cmd/                              # build and maintenance CLIs, not the app
  devtools/                       # dev settings → Wails/Vite environment bridge
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
    inbox_service.go              # item queries, triage, action-item decoding
                                  #   over store/ — no separate inbox/ package
    events/                       # typed bus
    flow/                         # flow YAML: parse, validate, save, watch, layout
      docs/                       # per-node-type markdown — read by the node drawer
                                  #   AND by an LLM (ADR 0009)
    runtime/                      # graph engine
      runtime.go                  #   package doc, Runner Options — no DB writes;
                                  #   durable KV reads via the KVReader port
      engine.go                   #   Engine: a runner per enabled flow, driven
                                  #   by App — installs, wakes, reloads, drains
      graph.go  run.go            #   index a flow, execute a batch -> CommitBatch
      kv.go                       #   the commit-atomic node-KV buffer
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
      sourcehttp/                 #   the HTTP toolkit every source client is
                                  #   built over — a lighter leaf (ADR 0018)
      github/                     #   Descriptor + Config + Factory
        feed/                     #   fetch layer: per-account response cache,
                                  #   conditional requests, rate-limit cooldown
        ghclient/                 #   the owned GitHub HTTP client (ADR 0015)
      webhook/                    #   Descriptor + Config + Factory; local ingress
    ingest/                       # producer loop, classification, absence, snapshots
      resolver.go                 #   the flow set -> live connector instances
    dispatch/                     # output worker, Dispatcher, executors; also
                                  #   where SystemNotifier (the notify port) is
                                  #   declared — consumer-defined, no notify/ package
    actions/                      # actions.yml catalog, watcher, editable model
      docs/                       # per-action-type markdown
    prompts/                      # Go-owned LLM prompt templates + registry (ADR 0009)
      templates/                 #   .tmpl files the registry renders
    skills/                       # install prompts as agent SKILL.md files; a
                                  #   per-target registry (Go path+body templates),
                                  #   a state-dir install index, hash-based drift
                                  #   sync that never clobbers a user edit (ADR 0033)
    credentials/                  # Ref{Provider, Account}, Store, keychain, index
    jobs/  activity/              # observability domains
    settings/                     # settings.yaml, paths, bootstrap pointer file
    store/                        # sqlc, migrations, queries

  adapter/                        # driving adapters, all in-process
    wailsui/                      # Wails service structs; the only Wails imports
      events.go                   # bus subscriber → Emit, per-event delivery policy
      windowservice.go  tray.go  focusstate.go  updater.go  notify.go
      e2e/                        # state-reset and smoke middleware
    httpapi/                      # REST + SSE, mounted via ServeHTTP at a Route.
                                  #   Built: an agent-facing control surface
                                  #   (read, reload, mutate — e.g. profile
                                  #   avatars, webhook node feed-mark images)
                                  #   on the shared loopback http server that also
                                  #   hosts the webhook listener (ADR 0021), in
                                  #   the errchain shape (ADR 0022): routes.go +
                                  #   ctrl_*.go per resource. One operations table
                                  #   backs the mux, GET /api, and a generated,
                                  #   validated GET /api/openapi.json (ADR 0027)
    mcpsrv/                       # tools over App; in-memory transport for the agent

  web/                            # HTTP plumbing shared with cmd/devserver
                                  #   (ADR 0022): error wire shape, version
                                  #   handler; mid/ (error + logger middleware),
                                  #   extractors/ (Body/Query decode + validate)

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

Five registries, all the same shape: an explicit map keyed by a type string,
declared in one file, with per-type config carrying its own `Validate` when it
has per-type config.

| Extension | Registry | Adding one means |
| --- | --- | --- |
| **Node type** | `app/flow` + `app/runtime` | config struct + `Inputs`/`Outputs`/`Validate` and one line in `flow`'s registry; one line in `runtime`'s behaviour registry saying what it does with a message (relay, sink, or process); `flow/docs/<type>.md`; plus `nodes/<type>/{config.ts,editor.vue,index.ts}` for the editor. A test fails if a type is in one registry and not the other |
| **Action type** | `app/actions` | config struct + `Validate`, one registry line, `actions/docs/<type>.md`, an `Executor`, one dispatcher line, the editable-catalog branch, and the YAML writer branch (`actionNode` in `store.go`) — the writer and the editable catalog both fail closed on a registered type with no branch, enforced by a registry-ranging roundtrip test |
| **Source connector** | `app/sources` | a `Descriptor`, a config struct with `Validate`, and a `Factory` — plus one line in `sources/registry.go` and one in `app`'s factory map. `flow`'s and `runtime`'s registries derive their entries, so neither is touched, and a test pins the Go registry against the frontend's `nodes/<type>/` directories. Still needs `flow/docs/<type>.md` and a `nodes/<type>/` editor entry until forms are schema-driven — but not a Settings ▸ Integrations entry: its presentation/drawer maps are an optional frontend nicety keyed by connector type, and a type they don't know still renders a generic card rather than being dropped (a spec pins that fallback), so a connector is functional in Settings before its presentation lands |
| **Script runtime** | `app/runtime` | a `ScriptRuntime` implementation and one registry line |
| **Skill target** | `app/skills` | one registry entry in `targets.go`: id, label, default directory, and path/body templates. The installer owns drift detection and sync semantics for every target, so adding an agent is data plus tests that the target renders |

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
- **Absence confirmation is batched, not per-item.** A connector's
  `AbsenceConfirmer` is asked once per tick over the whole absent set;
  verdicts return keyed by external id, not positionally, and a terminal
  verdict removes the item from the tracked set permanently. See ADR 0019.
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

The Wails adapter degrades most of these into the existing wake-up signals
(`log:appended`, `flows:updated`, …) where the frontend re-reads on receipt.
That contract is good for a GUI and stays. One event does not degrade:
`NotificationRaised` is delivered with `events.Buffer(32)` rather than
coalesced, and its payload crosses to the Wails event `notification:toast`
intact — a toast is the message itself, not a hint to go re-read something,
so there is nothing to degrade it to. This must not be pushed down into the
core generally: an MCP client cannot cheaply "re-read the service", and a
streaming CLI or SSE consumer needs the delta.

`application.Get()` must not appear outside `adapter/wailsui/`.

### Credentials

Secrets live in `app/credentials`, keyed by `Ref{Provider, Account}` and
stored in the OS keychain. Two constraints drive the design:

- **Keychains do not enumerate.** `List()` needs a separate index of refs
  (`<StateDir>/credentials.json`); only the values live in the keychain, and
  the two are reconciled lazily rather than on every read. `Get` is what
  reconciles: a ref present in the index whose keychain entry is gone reads as
  `ErrNotFound` and is pruned. `List` — what Settings ▸ Integrations calls —
  does not: it trusts the index outright, because verifying every entry
  against the keychain would turn a screen that enumerates connectors into one
  macOS permission prompt per provider. The accepted tradeoff: a credential
  lost outside the app (revoked in Keychain Access, a backup restored without
  it) can still read as "Connected" in Integrations until the next `Get` — the
  next fetch — self-heals it. There is no verify-on-open; the stale window is
  the cost of not prompting.
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
`HIVE_GRAFANA_TOKEN` both come for free. The override is provider-wide, not
per-account: while set, it authenticates every fetch for that provider
regardless of which account a source node names, which deliberately collapses
whatever multi-account isolation the store otherwise provides. That trade
serves one purpose — a headless run (CI, the server build, the e2e harness)
with one identity and no keychain to read — and is the wrong tool on a machine
actually juggling several accounts. Acquisition belongs to the connector:
GitHub's device flow lives in `app/sources/github`, while Grafana is a
secret-marked config field with no state machine. `Connection` is declared by
the connector that implements it, not by `app` — a connector needing no state
machine declares none.

**Multi-account is the model, not an extension of it.** One fetcher per
account: a `feed.LiveProvider` holds one account's response cache,
conditional-request state and rate-limit cooldown, so two accounts sharing one
would serve each other's items and stall each other's fetches. That cache is
what "nothing to invalidate" above leaves out: an in-app connect or disconnect
calls `Fetchers.InvalidateAll` so the next fetch never serves a stale
account's data, but a token rotated outside the app — edited directly in the
OS keychain — is not observed until the cache's own TTL (one poll interval, by
default) elapses on its own.

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

A profile's avatar is the worked example of an asset that straddles the two
(ADR 0025): the normalized PNG is app-local state at
`<StateDir>/assets/profiles/<flow-id>.png` (`internal/app/profileimg`), while
the flow YAML records only its content hash (`image:`). Config stays small and
text; a reference with no file — a config synced to a machine without its data
dir — reads as no image and the rail falls back to the letter chip, never an
error. The reference is owned by `FlowsService`'s image methods, not the graph
editor: `FlowStore.Save` preserves whatever the loaded flow declares, since the
editor round-trips only `{id, name, enabled, nodes, wires}` and would otherwise
drop the key.

A webhook source's image mark (ADR 0031) is the same asset shape with the
reference in a different place. Its normalized PNG is app-local state
(`internal/app/sourcemark`), keyed by **content hash** rather than by id because
it is uploaded before the graph save that records it; the webhook node's
`image:` config carries that hash and round-trips through the editor like any
other node field, so — unlike the profile avatar — it needs no preserve-on-save
seam. A missing file falls back to the node's glyph, the same tolerance, and
orphaned blobs are left in place rather than reference-counted.

`settings.yaml` is a nested typed document with `polling`, `updates`,
`notifications`, `appearance`, `webhooks`, `keybindings`, `skills`, and
`development` sections. Resolution is deterministic: safe compiled defaults, one strictly
decoded and validated YAML document, then typed
`HIVE_DESKTOP_<NAMESPACE>_<FIELD>` process overrides followed by effective-value
validation. Missing config is safe: webhooks and pprof
are disabled, listener hosts are loopback, automatic ports are `0`, mock mode
is live, and debug pauses are zero. Environment overrides affect the effective
value but are never written into YAML by an unrelated settings edit.

`settings.yaml`, `flows/*.yaml`, and `actions.yml` each carry a top-level
`version:` and are migrated forward in place at startup by
`internal/app/configmigrate` (ADR 0032) — a per-file, integer-versioned runner
distinct from the SQLite schema migrations (`internal/hivecore/data/migrate`)
that track applied versions in a table.

Paths resolve before settings because the config root determines where
`settings.yaml` lives. The fixed XDG `bootstrap.yaml` stores only `data_dir`
and `config_dir`; explicit `HIVE_DESKTOP_DATA_DIR` and
`HIVE_DESKTOP_CONFIG_DIR` values win, then bootstrap, then XDG defaults.
`desktop/main.go` derives one immutable `settings.Paths` snapshot — data and
config roots plus state, flows, actions, settings, credential-index and log
locations — and injects it. Runtime services do not re-read path environment variables.
`XDG_*`, Wails framework variables, credential secrets, build/release inputs,
and vendored Hive variables remain outside the desktop settings namespace.

Setup or `desktop:dev:prepare` creates or reuses the gitignored
`.hive-desktop/` directory in that worktree. Its config and data are isolated
from both the installed app and other worktrees; config symlink targets are
materialized rather than retained. `desktop:dev:fresh` reseeds it and
`desktop:dev:reset` removes it through marker-guarded deletion. An atomic
worktree lock serializes those operations, and destructive commands refuse
while either configured development server is active. `cmd/devtools prepare`
writes the resolved paths and ports to a gitignored, non-secret `launch.env`.
The `desktop:dev` mise task loads it followed by optional gitignored
`overrides.env`, then starts Wails directly. Devtools itself does not interpret
developer overrides.
Wails and Vite need ports before Go starts, so devtools resolves
`development.wails` and `development.vite` and bridges them to `WAILS_*`.
Wails hard-codes localhost in its frontend URL, so the Vite host is fixed to
`127.0.0.1`; the Wails server host may be any validated loopback address.
The OS keychain and fixed bootstrap pointer remain shared; use mock mode when
credential isolation matters.

Two databases remain separate on purpose: `hive.db` is shared with the
external `hive` CLI, and `desktop-pipeline.db` isolates desktop write traffic
from it. Their locations resolve independently: `desktop-pipeline.db` follows
`DataDir`, while `hive.db` follows `HiveDataDir` (defaulting to `DataDir`, so
production is unchanged). Development sets `HIVE_DESKTOP_HIVE_DATA_DIR` to the
installed hive data dir, so `hive.db` is shared in dev too while the desktop's
own state stays worktree-isolated. ADR 0014 records the configuration decision.

### Background lifecycle

Long-running subsystems — producer, output worker, retention, watchers, the
event bus, the webhook listener, the future HTTP and MCP servers — are meant
to register with a single `appkit/plugs` manager rather than each hand-rolling
`Start`/`Stop`, a `stopOnce`, and a teardown branch in `main`: uniform panic
capture, retry with backoff, and one graceful shutdown path.

**Adoption was attempted (2026-07-25) and declined.** `plugs.Manager.Start`
unconditionally installs `signal.NotifyContext`, which permanently adds Go's
process-wide `os/signal` watcher goroutine — `os/signal` offers no opt-out,
and `TestAppLifecycle`'s strict goroutine accounting rightly refuses the
leak. Plugs also guarantees no startup order, which the engine's
install-before-appenders constraint needs expressed, not worked around.
Until appkit offers signal opt-out and ordered start, the standing pattern
is **App-owned lifecycle**: every subsystem exposes an idempotent,
context-taking `Stop` behind a `stopOnce` (the webhook listener is the
template — ADR 0016), `App.Start` starts them in dependency order, and
`App.Close` unwinds them in reverse. `main` holds none of it.

`development.pprof` is typed and defaulted off. The endpoint has no lifecycle
of its own: when enabled, `httpapi.PprofHandler()` mounts on the shared
loopback HTTP server (ADR 0021) beside the agent API, torn down with it, so
there is no second listener and no teardown branch in `main` (ADR 0023). The
config is `{ enabled }` only — the address is the shared server's, so pprof
requires `http.enabled`, and a disabled endpoint has no route at all.

Other `appkit` packages with a clear home here: `httpclient` (context-first
client with composable middleware, **adopted** — see below) and `mapx`.

### Source HTTP

**Every source client is built over `sources/sourcehttp`** (ADR 0018), which
is itself built over `appkit/httpclient`. Nothing constructs a bespoke
`*http.Client` to reach a provider API.

`sourcehttp` owns four things:

- **The failure taxonomy** — `ErrUnauthorized`, `ErrRateLimited`,
  `ErrUnreachable`, and `RateLimitError` with the server's reset time. This is
  the app's vocabulary, not a provider's: the Integrations screen maps
  unauthorized onto re-auth, and a provider pauses fetching on rate limited. A
  connector that classifies into these three gets both behaviours without the
  app learning its name. It lives here rather than in `sources/connector`
  because `connector` reaches `app/store` and would drag the SQLite driver
  into every client package.
- **Status and rate-limit mapping** — `Errors.Status` maps a response onto the
  taxonomy, with a provider-supplied hook for APIs that overload 403.
  Rate-limit hints read `Retry-After` first, then `X-RateLimit-Reset`.
- **Conditional requests** — `Validators` carries ETag and Last-Modified
  between a response and the next request. An authenticated 304 is free of
  rate-limit cost, so polling an unchanged resource costs nothing.
- **A logging transport** — request, status, elapsed, and
  `X-RateLimit-Remaining` at debug level, with secret-looking query parameters
  redacted. It is a `RoundTripper`, not middleware, so it observes the request
  that reaches the wire regardless of what a caller stacks in front; a
  cancelled request logs at debug so shutdown writes no failure lines.

What it deliberately does **not** own: response caching, poll cadence, and
rate-limit cooldowns (source semantics — they stay in the provider, e.g.
`github/feed`), credential resolution (already generic in `app/credentials`),
and provider vocabulary such as GraphQL batching or an OAuth device flow.
Retry, backoff, and pagination are absent until a second connector shows what
they should look like — the package is an extraction from one implementation
and should grow by evidence, not by anticipation.

The app's other outbound HTTP — the updater, `cmd/release`, and the
problem-report uploader (`report.Uploader`, ADR 0024) — is not a source and has
not adopted it.

## Execution model

The flow engine runs **in Go**, in-process. Source polling, graph routing,
script evaluation, and action execution all happen in the core; the frontend
owns the editor, not the runtime.

This replaces a split model in which Go ingested and executed while the
browser routed. That split made the desktop window a hard dependency of flow
execution and put the correctness-critical parts — topological order,
fan-out, offset advancement, commit atomicity — out of reach of any headless
surface. Consolidating in Go is what makes a `dry-run` surface *possible* —
one execution path an editor preview, a CLI, and an MCP tool could each drive
identically. No such surface is built yet; what exists is the separation that
would let one be added without a second implementation of routing semantics.

`Runner.Run` returns the `CommitBatch` a batch of messages is worth and
**does not commit it**. Reading the log and applying the batch belong to the
caller, which is what would let a dry-run and a live tick be one code path
rather than two implementations of the same semantics, once a caller asks for
one without committing.

The engine never *writes* the database. Its one read path is the `KVReader`
driven port behind a function node's `kv` object — durable per-node key-value
state (the `node_kv` table), read live during a tick and written back as
`CommitBatch.KVMutations` so a script's writes are durable iff its tick
commits, with per-message staging discarding the writes of a message that
threw. `kv` is `state`'s durable sibling: `state` is in-memory scratch per
deploy, `kv` is the dedup/change-detection memory that survives restarts,
reconciled against the flow's KV-capable node ids inside `ActivateReplay`'s
transaction and read as absent during replay so membership recompute stays a
pure function of snapshots and graph. Entries are size-capped but not
count-capped — growth is bounded by TTL and teardown, never by pruning live
keys, because a pruned seen-set re-notifies. Only notify nodes notify — feeds are
pure membership surfaces — and dedup for the notify branch belongs in a
function node backed by `kv`, not in the delivery path (whose only noise
control is a per-node, per-item delivery cooldown).

Two registries describe a node type between them: `flow`'s says how it is
configured, `runtime`'s says what it does when a message arrives. Neither the
router nor the executor branches on a type string.

`runtime.Engine` — a field on `App`, not a type in package `app` — is what
drives it. It is level-triggered: `Wake` and `Reload` set a latch rather than
queueing, so a signal arriving mid-pass is serviced by the next pass instead
of being dropped or piling up. Installation is synchronous in `Start`, before
anything that can append to the log is running, so no append can arrive with
no runner to route it.

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
8. **New background work joins the App-owned lifecycle**: an idempotent,
   context-taking `Stop` behind a `stopOnce`, started in `App.Start`, unwound
   in `App.Close`. No teardown branches in `main`. Plugs remains the target
   once appkit unblocks it — see [Background lifecycle](#background-lifecycle).
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
   **Done.** `runtime.Engine`, driven by `App`, drives the runners and owns
   the replay protocol; `PipelineService` no longer carries `ReadFrom`,
   `Commit`, `EventLogTailOffset`, `ActivateReplay`, `ListReplaySourceSnapshots`
   or `ListUnarchivedInboxItems`. `CommitBatch.UpToOffset` went with them: it
   is a plain `int64` now, not the decimal-string encoding those RPCs needed to
   survive Wails' JSON bridge. `Msg.ID` stays a string, but for an unrelated
   reason that outlived the RPCs — the goja script boundary, where a JS number
   would truncate an offset past 2^53 (see [Script nodes](#script-nodes)).
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
7. **Adapters** — HTTP and MCP mounted in-process; plugs for lifecycle
   (the lifecycle half is blocked on appkit — see
   [Background lifecycle](#background-lifecycle)). **In progress:** the HTTP
   slice is an agent-facing control surface (read, reload, and mutations like
   profile avatars) on a shared loopback `http` server (on by default) that
   also hosts the webhook listener (ADR 0021), growing toward full agentic
   control; the full REST + SSE surface and MCP are still to come.

### Data that must survive

Breaking changes to DB schema are acceptable and carried forward by the
table-tracked SQLite migration runner. Breaking changes to config **format**
(`settings.yaml`, `flows/*.yaml`, `actions.yml`) are carried forward the same
way, but by the separate per-file migration runner (ADR 0032) rather than by
breaking. Three things are not rebuildable and must be carried across any
migration:

- `inbox_item.unread` / `archived_at` / `archived_actor` / `archived_reason` —
  triage decisions, not derivable from any source.
- `output_command`'s unique `(action_id, key)` index — the only thing
  preventing an already-run action from re-firing.
- `node_kv` — a function node's dedup/change-detection memory. Replay is
  deliberately KV-inert, so a lost seen-set cannot be recomputed; dropping
  it re-fires every notify-once branch.

Everything else (`event_log`, `feed_membership_claim`, `node_run`,
`source_head`, `consumer_offset`, `activity_event`, `job`) is derived or
replayable. Migrations are not squashed, because a squashed `0001` would not
match existing `schema_migrations` rows.

## Open questions

These are deliberately unresolved; revisit when the relevant work starts.

- **Command placement** — per-domain service methods with request structs
  (current plan, matching `hivecore/hive/app.go`) versus a flat
  `app/command` + `app/query` package that gives MCP and CLI generation one
  place to enumerate. The httpapi operations table (ADR 0027) is an
  adapter-local precedent for the enumeration side, not a resolution of where
  the commands live.
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
