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
> append (ADRs goja-script-runtime and flow-engine-in-go). The browser engine is gone, and with it the six
> RPCs that existed only to feed it. Flow execution no longer depends on a
> window being open.
>
> Source connectors are declared: `internal/app/sources` holds a registry of
> `connector.Descriptor`s — type, title, pull/push mode, stability, declared
> capabilities, config schema — and `connector.Factory` constructs instances
> where their dependencies live. The producer reads a capability off the
> instance instead of type-asserting for it, and `flow`'s node registry and
> `runtime`'s behaviour registry both derive their source entries from it, so
> adding a connector is a change to `sources/` alone (ADR source-connector-registry). GitHub's
> connector owns its HTTP client end to end (`sources/github/ghclient`) rather
> than routing through the vendored `hivecore/github` package, and nothing
> outside `internal/hivecore` imports that package anymore (ADR owned-github-client).
>
> Credentials are keyed by account: `app/credentials` stores a value per
> `Ref{Provider, Account}` in the OS keychain with a separate index of refs,
> a source node names the account it fetches as, and lookup is generic while
> acquisition stays with the connector. GitHub is one connector among them
> rather than a login — nothing is gated on being connected to it, and
> Settings ▸ Integrations is a projection of the same registry (ADR credential-store).
>
> Desktop configuration is one nested typed schema: startup resolves safe
> defaults, strict YAML and `HIVE_DESKTOP_*` overrides once, then injects the
> resulting settings and immutable path snapshot. Development state is local to
> each worktree under `.hive-desktop/` (ADR desktop-configuration).
>
> The agent-facing surface is MCP: `internal/adapter/mcpsrv` serves the app's
> capabilities as tools over `app.App`, on the same single loopback `http`
> server (on by default) that hosts the webhook listener (ADR agent-http-api). It is
> stateless, so it holds nothing between requests and joins no lifecycle, and
> it is unauthenticated behind the loopback bind because it spawns nothing
> (ADR mcp-replaces-the-agent-facing-http-api). It replaced the REST control surface that preceded it, whose
> routes and generated OpenAPI document are deleted rather than deprecated —
> `tools/list`, inferred from the same Go types the handlers take, is what
> makes it self-describing now.
>
> `internal/adapter/httpapi` remains, narrowed to two things that are not an
> agent surface: the Wails frontend's terminal, pop-up-terminal and
> agent-workspace control planes under `/api/terminal/` (token-guarded, because
> each spawns a process), and `/api/status` + `/api/version` as the plain-GET
> liveness probe. The full REST + SSE product surface is still absent.
>
> Terminal mode is the second driving transport: `internal/app/tmuxcc` is a
> transport-free tmux control-mode client with an App-owned lifecycle,
> `app.TerminalsService` is the slug-keyed driving service, `httpapi` carries
> both the REST control plane and the per-session binary WebSocket data plane on
> the same loopback server, and the wailsui `TerminalService` gates the feature
> and bootstraps the webview (ADR terminal-transport). See
> [Terminal sessions](#terminal-sessions).
>
> Agent workspaces are a third driving surface, behind the same terminal
> transport rather than a new one: `internal/app/agentws` owns a
> generated-and-disposable on-disk root (ADR workspace-directories-are-generated-and-disposable) and drives sessions as tmux
> sessions named `agentws-<record id>` — not hive ones, but riding the same
> `tmuxcc.Manager` and tmux stream a hive session's terminal does, which is
> what lets a session survive an app restart (ADR agent-workspace-sessions-are-tmux-sessions); its control plane
> rides `httpapi`'s `/api/terminal/` prefix and authenticates per handler
> because it spawns processes too. `internal/app/mcpcatalog` is the shipped
> MCP server registry it wires workspaces against. The whole area ships dark
> behind `experimental.agents` (ADR a-workspace-declares-its-own-authority). See
> [Agent workspaces](#agent-workspaces).
>
> Not yet built: the plugs-managed lifecycle (attempted; blocked on appkit —
> see [Background lifecycle](#background-lifecycle)). New work should move
> toward this shape rather than extending the current one.

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
| **Error chain** (httpkit `errchain`) | every HTTP surface: `httpapi`, devserver control | Handlers are `func(w, r) error` behind one `web/mid.Errors` middleware that maps error types to responses exactly once — no handler writes a status inline. Input enters only through `web/extractors` (`Body` decode + the struct's criterio `Validate`). Per-resource `ctrl_*.go` files, routes registered in one place. See ADR http-handler-conventions. |
| **Tool table** | `mcpsrv` | One file declares every MCP tool — name, title, description — and nothing else; the handler beside it is a thin call into `App`. Input schemas are *inferred from the handler's typed input struct*, never hand-written, so a tool cannot advertise a field its handler does not accept. A store type whose `jsonschema` tags were written for the OpenAPI reflector cannot be a tool's input or output type: the SDK's inferrer rejects a `WORD=`-prefixed tag, and `json.RawMessage` infers as an array. Declare an adapter-local type and convert at the seam. Three contracts hold across the whole surface, because an agent has no UI to disambiguate from: an id that resolves to nothing is `not_found` and never an empty collection; a mutation's answer is never a constant, so a caller can tell it happened; and a field whose size the *source* decides — an item payload, an event detail, a dry run's messages — is behind a `detail` argument that defaults to omitting it, with the level echoed on the answer. See ADR mcp-replaces-the-agent-facing-http-api. |
| **Data-plane mount** | streaming surfaces on the loopback server: the terminal WebSocket | A surface that streams bytes is a raw `http.Handler` mounted at its own prefix via `App.MountAPI` — never a row in the errchain operations table, which cannot frame a hijacked socket. Its request/response half stays REST on `httpapi`; only what needs latency or backpressure rides the socket. It authenticates itself if it must, because the errchain surface around it is deliberately unauthenticated. See ADR terminal-transport. |
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
| **Single declaration, many consumers** | node and action types, later connector config | One Go declaration — schema plus prose — feeds the editor form, the node drawer, and an LLM. A bijection test fails if a registered type has no doc. ADR go-owned-llm-prompts. This is the pattern every new extension point should extend. |
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
| A new **MCP server type** | Registry, Single declaration (`Descriptor` + `mcpcatalog/docs/<type>.md`) | [Extension points](#extension-points) |
| A new **script language** | Strategy behind the `ScriptRuntime` port, Registry | [Script nodes](#script-nodes) |
| A new **bound method / RPC** | Facade, Adapter, Typed errors | [Placement rules](#placement-rules), rules 1–4 |
| A new **HTTP, MCP or CLI surface** | Adapter, Ports & Adapters (driving side — no interface) | [The Go amendment](#the-go-amendment-to-hexagonal) |
| A new **agent capability** | Tool table — one row in `mcpsrv/tools.go`, a typed input struct, a thin `App` call | ADR mcp-replaces-the-agent-facing-http-api |
| A new **HTTP endpoint** | Error chain — `errchain` handler, `web/extractors` input, `ctrl_*.go` + routes in one place. Only for the frontend's own transport: an agent capability is a tool | ADR http-handler-conventions, ADR mcp-replaces-the-agent-facing-http-api |
| A new **streaming endpoint** (WebSocket/SSE) | Data-plane mount — raw handler at its own prefix, REST control plane beside it | [Terminal sessions](#terminal-sessions), ADR terminal-transport |
| A new **event** | Observer — payload in core, degraded to a wake-up in `wailsui` | [Events](#events) |
| A new **background subsystem** | One instance per process, App-owned lifecycle (plugs once unblocked) | [Background lifecycle](#background-lifecycle) |
| A new **app mode** | Closed union over sibling active flags — never an `else` branch | [App modes](#app-modes) |
| A new **persisted field** | Config-vs-data boundary; Value Object for anything secret-bearing | [Config versus data](#config-versus-data), [Credentials](#credentials) |
| An operation **spanning two domains** | Unit of Work — `db.Ctx(ctx)` to join the ambient transaction, never a second one | [Config versus data](#config-versus-data) |
| A new **dependency on something outside** | Consumer-defined interface in the package that calls it | [Layers and the dependency rule](#layers-and-the-dependency-rule) |
| Anything touching **vendored code** | Anti-Corruption Layer, Bounded Context — wrap, never edit | [Layers and the dependency rule](#layers-and-the-dependency-rule) |
| A new **outbound HTTP call from a source** | `sources/sourcehttp` over `appkit/httpclient` — never a bespoke client | [Source HTTP](#source-http) |
| A new **command the app spawns on the user's behalf** | Resolved environment — `execenv` supplies `Cmd.Env` and resolves the binary; never the inherited PATH, and never a login shell in place of it | [Subprocess environment](#subprocess-environment) |
| A **breaking config schema change** | Forward-only YAML migration runner (per-file `version:`, comment-not-preserving rewrite, backup under StateDir) | [Config versus data](#config-versus-data), ADR yaml-config-migration |

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
                                  #   AND by an LLM (ADR go-owned-llm-prompts)
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
    fonts/                        # the installed-family scan the app and
                                  #   terminal font pickers read — every family
                                  #   plus the fixed-pitch subset, one cached
                                  #   pass (ADRs terminal-typography-is-configurable,
                                  #   the-app-s-faces-are-picked-from-installed-fonts-not-from-a-bundled-set)
    icons/                        # curated feed glyph set — a leaf, shared by
                                  #   flow's feed node and the webhook connector
    sources/                      # connector registry
      registry.go                 #   the map of descriptors, in one file
      connector/                  #   the vocabulary — a leaf, so a connector can
                                  #   name it without importing the registry back
      sourcehttp/                 #   the HTTP toolkit every source client is
                                  #   built over — a lighter leaf (ADR source-http-toolkit)
      github/                     #   Descriptor + Config + Factory
        feed/                     #   fetch layer: per-account response cache,
                                  #   conditional requests, rate-limit cooldown
        ghclient/                 #   the owned GitHub HTTP client (ADR owned-github-client)
      webhook/                    #   Descriptor + Config + Factory; local ingress
    ingest/                       # producer loop, classification, absence, snapshots
      resolver.go                 #   the flow set -> live connector instances
    dispatch/                     # output worker, Dispatcher, executors; also
                                  #   where SystemNotifier (the notify port) is
                                  #   declared — consumer-defined, no notify/ package
    actions/                      # actions.yml catalog, watcher, editable model
      docs/                       # per-action-type markdown
    prompts/                      # Go-owned LLM prompt templates + registry (ADR go-owned-llm-prompts)
      templates/                 #   .tmpl files the registry renders
    mcpcatalog/                   # the shipped MCP server catalogue: Descriptor +
                                  #   registry, following the connector precedent
                                  #   (ADR source-connector-registry) with no config factory — a shipped
                                  #   entry carries none in M1
      docs/                       #   per-MCP-type markdown — read by the catalogue
                                  #   AND by an LLM (ADR go-owned-llm-prompts)
    agentws/                      # the agent-workspace root: mcps.yaml, .shared/,
                                  #   one directory per workspace; Workspace/Library
                                  #   parse+validate, the generator, the launch table
                                  #   (autonomy flags, MCP wiring), the two-level
                                  #   watcher (ADR a-workspace-declares-its-own-authority, ADR workspace-directories-are-generated-and-disposable)
    skills/                       # install prompts as agent SKILL.md files; a
                                  #   per-target registry (Go path+body templates),
                                  #   a state-dir install index, hash-based drift
                                  #   sync that never clobbers a user edit (ADR skill-installer)
    credentials/                  # Ref{Provider, Account}, Store, keychain, index
    tmuxcc/                       # tmux control-mode client: line framer, command
                                  #   FIFO, %output decode, one client per session
                                  #   slug, fan-out broker — no transport, no UI
    tmuxbin/                      # where the tmux binary is: paths.tmux, then
                                  #   PATH, then package prefixes (ADR tmux-discovery)
    ptyterm/                      # ephemeral terminals: a PTY and the process on
                                  #   the far end, id-keyed, dying with the app —
                                  #   what the pop-up runs on (ADR ephemeral-popup-terminals)
    execenv/                      # the environment the user's own commands run
                                  #   in: the login shell's PATH, then this
                                  #   process's, then those prefixes (ADR subprocess-environment)
    jobs/  activity/              # observability domains
    perf/                         # UI performance spans -> a size-capped JSONL
                                  #   file; development-gated, no aggregation
                                  #   and no dependencies (ADR ui-performance-spans-are-recorded-to-jsonl)
    settings/                     # settings.yaml, paths, bootstrap pointer file
    store/                        # sqlc, migrations, queries

  adapter/                        # driving adapters, all in-process
    wailsui/                      # Wails service structs; the only Wails imports
      events.go                   # bus subscriber → Emit, per-event delivery policy
      windowservice.go  tray.go  focusstate.go  updater.go  notify.go
      terminalservice.go          # Available + Endpoint: the terminal's gate and
                                  #   webview bootstrap (ADR terminal-transport)
      e2e/                        # state-reset and smoke middleware
    httpapi/                      # Mounted via ServeHTTP at a Route on the shared
                                  #   loopback http server that also hosts the
                                  #   webhook listener (ADR agent-http-api), in the errchain
                                  #   shape (ADR http-handler-conventions): routes.go + ctrl_*.go per
                                  #   resource, one operations table backing the
                                  #   mux. NOT an agent surface — that moved to
                                  #   mcpsrv (ADR mcp-replaces-the-agent-facing-http-api). What is left: the terminal
                                  #   control plane as rows on that table, its
                                  #   per-session WebSocket data plane as a separate
                                  #   raw mount (ADR terminal-transport), the agent-workspace
                                  #   control plane under the same /api/terminal/
                                  #   prefix authenticating per handler because it
                                  #   spawns processes too (ADR a-workspace-declares-its-own-authority), and
                                  #   /api/status + /api/version as the plain-GET
                                  #   liveness probe
    mcpsrv/                       # THE AGENT SURFACE: tools over App, served over
                                  #   Streamable HTTP at /mcp on that same loopback
                                  #   server (ADR mcp-replaces-the-agent-facing-http-api). tools.go is the tool table
                                  #   — metadata only, one place; tool_*.go per
                                  #   domain hold typed inputs and thin App calls;
                                  #   errors.go maps app.Kind once. Stateless, so
                                  #   no session outlives a request and there is no
                                  #   lifecycle to unwind; unauthenticated behind
                                  #   the loopback bind because nothing here spawns
                                  #   a process

  web/                            # HTTP plumbing shared with cmd/devserver
                                  #   (ADR http-handler-conventions): error wire shape, version
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

Six registries, all the same shape: an explicit map keyed by a type string,
declared in one file, with per-type config carrying its own `Validate` when it
has per-type config.

| Extension | Registry | Adding one means |
| --- | --- | --- |
| **Node type** | `app/flow` + `app/runtime` | config struct + `Inputs`/`Outputs`/`Validate` and one line in `flow`'s registry; one line in `runtime`'s behaviour registry saying what it does with a message (relay, sink, or process); `flow/docs/<type>.md`; plus `nodes/<type>/{config.ts,editor.vue,index.ts}` for the editor. A test fails if a type is in one registry and not the other |
| **Action type** | `app/actions` | config struct + `Validate`, one registry line, `actions/docs/<type>.md`, an `Executor`, one dispatcher line, the editable-catalog branch, and the YAML writer branch (`actionNode` in `store.go`) — the writer and the editable catalog both fail closed on a registered type with no branch, enforced by a registry-ranging roundtrip test. **Envelope fields are not part of that checklist**: `targets`, `applies_to`, `show_in_detail` and the declared `inputs` a new type inherits for free, because every type renders over the same `OutputData` (ADR action-declared-inputs, ADR actions-target-terminal-sessions-and-windows). A type that cannot serve a terminal target says so in `TerminalCapable`, beside `HeadlessCapable`, and `validateActions` refuses the declaration. A thing that does not dispatch at all is not an action type: the pop-up launchers are their own list in the same file, with their own struct and no envelope (ADR launchers-are-their-own-list-in-actions-yml) |
| **Source connector** | `app/sources` | a `Descriptor`, a config struct with `Validate`, and a `Factory` — plus one line in `sources/registry.go` and one in `app`'s factory map. `flow`'s and `runtime`'s registries derive their entries, so neither is touched, and a test pins the Go registry against the frontend's `nodes/<type>/` directories. Still needs `flow/docs/<type>.md` and a `nodes/<type>/` editor entry until forms are schema-driven — but not a Settings ▸ Integrations entry: its presentation/drawer maps are an optional frontend nicety keyed by connector type, and a type they don't know still renders a generic card rather than being dropped (a spec pins that fallback), so a connector is functional in Settings before its presentation lands |
| **Script runtime** | `app/runtime` | a `ScriptRuntime` implementation and one registry line |
| **Skill target** | `app/skills` | one registry entry in `targets.go`: id, label, default directory, and path/body templates. The installer owns drift detection and sync semantics for every target, so adding an agent is data plus tests that the target renders |
| **MCP server type** | `app/mcpcatalog` | a `Descriptor` carrying a fixed `Server` (transport, command/args/env or url/headers) and one registry line, plus `mcpcatalog/docs/<type>.md` — a registry↔docs bijection test fails an entry with no doc. This is the connector precedent (`app/sources`) extended minus its config factory: a shipped entry carries no per-workspace configuration in M1, so there is nothing for a factory to construct |

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
should feed the editor form from the same source it already feeds the node
drawer from. Note that an MCP tool's input schema is *not* reflected from these
declarations: the SDK's inferrer reads the `jsonschema` tag differently and
rejects the invopop form, so `mcpsrv` declares its own input types (ADR mcp-replaces-the-agent-facing-http-api).

### Source connectors

The connector interface is the one most likely to age badly, so it is
specified rather than left to grow. ADR source-connector-registry records why.

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
  verdict removes the item from the tracked set permanently. See ADR batched-absence-confirmation.
- **A failed `Produce` is not an empty snapshot.** A successful call is
  authoritative, so returning nil on a broken fetch archives every item the
  source owns. A connector that can fail partway decodes its whole result
  before the first `emit`, so a run cannot half-succeed — and where truncation
  is possible it fails rather than truncates, because truncated-but-parseable
  output *is* a short snapshot. ADR a-command-is-a-source.
- **Cadence is a floor on the instance, not a second scheduler.** There is one
  ticker (`settings.polling.interval`); an instance whose cost does not suit it
  sets `Instance.MinInterval` and the producer skips it until it is due,
  quantized to the tick. Not drained is not drained-empty: `Produce` is not
  called, so nothing about the tracked set changes. `Producer.Refresh` — what a
  manual refresh calls — ignores every floor. Zero, the default, is every tick.
- **Config references credentials, never embeds them.** See below.
- **Never mirror the upstream API's shape in connector config.** Provider
  vocabulary leaking into the flow schema is permanent.
- **A payload the user shapes uses the canonical item contract, once.**
  `sources/canonical` holds the `state` vocabulary and the classifier that maps
  it to lifecycle. A connector whose payload is user-authored JSON (a webhook
  delivery, a command's stdout) names that classifier rather than interpreting
  "resolved" a second way — the same contract must mean the same thing whichever
  door an item came through.
- **A connector that runs the user's command runs a fixed string.** Nothing
  ingested may be templated into it: `sources.exec` is as trusted as the flow
  file it is written in, and only that. Config directories already carry
  `actions.yml`'s shell executor and `function` nodes' JavaScript, so the
  boundary is the directory, not the node — but a command built from fetched
  data would move it, which is why it is forbidden rather than discouraged
  (ADR a-command-is-a-source).

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
the credential store holds. ADR credential-store records why this is a new store rather
than an extension of the vendored single-slot one.

### Config versus data

User-editable config (`flows/`, `actions.yml`, `settings.yaml`) lives under
`$XDG_CONFIG_HOME/hive/desktop/` so it can be dotfiles-managed. App-local
state (SQLite: items, triage, offsets, queued commands) lives under the data
dir. Respect the boundary when adding persistence.

The agent-workspace root (`AgentWorkspacesDir`, default
`<ConfigDir>/workspaces`) is the one place this boundary is deliberately
crossed: it resolves under the config root so it can be dotfiles-/iCloud-synced
like `flows/`, but the directory itself is not small text — an agent writes
into it and the generator reconciles disposable derived files alongside the
authored ones (ADR workspace-directories-are-generated-and-disposable) — because ADR profile-images's hash-in-config/blob-in-data
asset split does not apply to a directory the agent itself writes into (D3).

A profile's avatar is the worked example of an asset that straddles the two
(ADR profile-images): the normalized PNG is app-local state at
`<StateDir>/assets/profiles/<flow-id>.png` (`internal/app/profileimg`), while
the flow YAML records only its content hash (`image:`). Config stays small and
text; a reference with no file — a config synced to a machine without its data
dir — reads as no image and the rail falls back to the letter chip, never an
error. The reference is owned by `FlowsService`'s image methods, not the graph
editor: `FlowStore.Save` preserves whatever the loaded flow declares, since the
editor round-trips only `{id, name, enabled, nodes, wires}` and would otherwise
drop the key.

A webhook source's image mark (ADR webhook-source-image-marks) is the same asset shape with the
reference in a different place. Its normalized PNG is app-local state
(`internal/app/sourcemark`), keyed by **content hash** rather than by id because
it is uploaded before the graph save that records it; the webhook node's
`image:` config carries that hash and round-trips through the editor like any
other node field, so — unlike the profile avatar — it needs no preserve-on-save
seam. A missing file falls back to the node's glyph, the same tolerance, and
orphaned blobs are left in place rather than reference-counted.

`settings.yaml` is a nested typed document with `polling`, `updates`,
`notifications`, `appearance`, `http`, `keybindings`, `skills`,
`experimental`, and
`development` sections. `experimental` holds ships-dark feature opt-ins
(ADR terminal-experimental-gate), each read once at startup and defaulting to off. Resolution is deterministic: safe compiled defaults, one strictly
decoded and validated YAML document, then typed
`HIVE_DESKTOP_<NAMESPACE>_<FIELD>` process overrides followed by effective-value
validation. Missing config is safe: webhooks and pprof
are disabled, listener hosts are loopback, automatic ports are `0`, mock mode
is live, and debug pauses are zero. Environment overrides affect the effective
value but are never written into YAML by an unrelated settings edit.

`settings.yaml`, `flows/*.yaml`, and `actions.yml` each carry a top-level
`version:` and are migrated forward in place at startup by
`internal/app/configmigrate` (ADR yaml-config-migration) — a per-file, integer-versioned runner
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

Setup or `dev:prepare` creates or reuses the gitignored
`.hive-desktop/` directory in that worktree. Its config and data are isolated
from both the installed app and other worktrees; config symlink targets are
materialized rather than retained. The installed agent-workspace root is
snapshotted separately into `.hive-desktop/config/workspaces`, and `launch.env`
pins `HIVE_DESKTOP_AGENT_WORKSPACES_DIR` there so an installed iCloud/File
Provider location cannot leak into a dev process. `dev:fresh` reseeds
it and `dev:reset` removes it through marker-guarded deletion. An atomic
worktree lock serializes those operations, and destructive commands refuse
while either configured development server is active. `cmd/devtools prepare`
writes the resolved paths and ports to a gitignored, non-secret `launch.env`.
The `dev` mise task loads it followed by optional gitignored
`overrides.env`, then starts Wails through `cmd/devtools run`. Devtools itself
does not interpret developer overrides.

`devtools run` supervises the dev runner because the runner does not supervise
itself on the way out (ADR shutdown-is-signalled-and-bounded): it puts the app and Vite in process groups of
their own, so a closing terminal signals neither, and the runner dies on SIGHUP
before it can tear them down — leaving the app running with no dev session
behind it. The supervisor answers the signal instead: it asks the app to shut
down, waits for `App.Close`, interrupts the runner, and sweeps what is left.
Rebuild-and-restart stays the runner's; only teardown moves.
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
own state stays worktree-isolated. ADR desktop-configuration records the configuration decision.

### Settings panes

Application settings are sectioned by **the surface a value changes**, not by
kind, and the nav groups are the app's own modes (ADR settings-sections-name-the-surface-they-change):

| Group | Sections |
| --- | --- |
| Preferences | General · Appearance · Notifications · Keyboard |
| Inbox | Integrations · Actions |
| Code | Terminal · Quick terminals |
| Agents | Agents · Skills |
| Advanced | System · About |

A value one surface uses lives on that surface's pane; a value several use lives
in **General** (the editor command); **System** is this install — storage,
diagnostics, the problem reporter; **About** is the running build. There is no
leftover group — a section that fits nowhere means the grouping is wrong.
Experimental is a posture, not a category: a ships-dark opt-in (ADR terminal-experimental-gate)
renders on the pane for the feature it gates, through
`settings/ExperimentalToggle.vue`.

The section list is one list. `applicationSettingsSections` in `router.ts`
builds the route matcher and backs `isApplicationSettingsSection`, which
`App.vue` uses to resolve `:section`; adding or renaming a section is that list
plus `categoryMeta` and `navGroups` in `SettingsView.vue`, and a spec asserts
every section appears in exactly one nav group so a partial edit fails rather
than shipping a pane that is routable but unreachable. Error and empty-state
copy that names a pane is part of the section's contract — moving one means
updating the strings that point at it.

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
template — ADR webhook-listener-placement), `App.Start` starts them in dependency order, and
`App.Close` unwinds them in reverse. `main` holds none of it.

`development.pprof` is typed and defaulted off. The endpoint has no lifecycle
of its own: when enabled, `httpapi.PprofHandler()` mounts on the shared
loopback HTTP server (ADR agent-http-api) beside the agent API, torn down with it, so
there is no second listener and no teardown branch in `main` (ADR pprof-debug-endpoint). The
config is `{ enabled }` only — the address is the shared server's, so pprof
requires `http.enabled`, and a disabled endpoint has no route at all.

Other `appkit` packages with a clear home here: `httpclient` (context-first
client with composable middleware, **adopted** — see below) and `mapx`.

### Source HTTP

**Every source client is built over `sources/sourcehttp`** (ADR source-http-toolkit), which
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
problem-report uploader (`report.Uploader`, ADR in-app-problem-reporting) — is not a source and has
not adopted it.

### Subprocess environment

On macOS the signed app is the TCC responsible process for tmux, shells, agents,
and their descendants. Both Darwin bundle variants carry the child-process
privacy usage descriptions, and every signing path applies the Apple Events
entitlement; access is still granted by the user when macOS prompts, never by an
entitlement. Full Disk Access is optional for tools that routinely enter other
apps' protected containers (ADR
[hive-acts-as-the-macos-tcc-responsible-process-for-terminal-children](decisions/2026-08-07-hive-acts-as-the-macos-tcc-responsible-process-for-terminal-children.md)).

**A command the user wrote runs in the PATH the user has, not the one the app
inherited** (ADR subprocess-environment). A desktop launch's environment is the launcher's —
macOS gives an `.app` bundle `/usr/bin:/bin:/usr/sbin:/sbin` — and session hooks
and shell actions are an open set of user commands, so no list of prefixes
substitutes for asking. `internal/app/execenv` asks the login shell once per run
(`$SHELL -ilc /usr/bin/env`, timed out, the answer kept whether or not it
worked) and layers this process's PATH and the ADR tmux-discovery prefixes behind it; a
probe that fails degrades to exactly what the app could reach before.

Three rules follow for anything new that spawns a process on the user's behalf:

- **Take the environment from `execenv`, and resolve the binary through it too.**
  `os/exec` searches the *calling* process's PATH and ignores `Cmd.Env`, so
  setting the environment alone still fails to start a command only the resolved
  PATH knows about. `app.envExecutor` is the worked example; it is also why
  Hive's vendored `executil.RealExecutor` is not used.
- **A login shell in front of the command is not a substitute for it** (ADR
  0068). `$SHELL -l -c <line>` — what both terminal backends run a command
  through — is a login shell but not an interactive one, so zsh reads
  `.zprofile` and never `.zshrc`, which is where a PATH is as often set. Both
  backends therefore take an `Environ` hook wired to the resolver
  (`tmuxcc.ManagerOptions.Environ`, `ptyterm.ManagerOptions.Environ`), and the
  hook is written twice rather than extracted: ADR ephemeral-popup-terminals keeps the two backends
  siblings with no shared interface. What the resolver supplies is the **floor**
  — the login startup files that do run may still override it. For tmux the hook
  reaches the client rather than the session, because a pane inherits the
  environment of the client that created it; setting `Cmd.Env` on the
  `new-session` command is what puts an agent binary on its PATH, and no
  `new-session -e` plumbing is involved.
- **A streamed command's failure carries the opening of its stderr.** Hive
  streams hook output to `io.Discard`, so without it a missing command reaches
  the jobs list as an exit status naming nothing.
- **A command on a timer takes the resolver's answer, never its own probe**
  (ADR a-command-is-a-source). `sources.exec` runs on every poll tick, so `$SHELL -ilc` per run
  would charge each one the user's version-manager initialization and make a
  slow rc file a randomly-blown timeout. The resolver probes once per run and
  remembers; that is the whole point of it. Aliases are the deliberate cost —
  they are interactive-shell sugar, and a config file that depends on one is not
  reproducible on another machine.

The probe answers more than PATH. **A hive config override the CLI takes from the
environment is read through `execenv.Getenv`, never `os.Getenv`** (ADR hive-env-overrides-resolve-through-the-login-shell):
the launch that has no PATH has no `HIVE_DEFAULT_AGENT` either, and hive folds
that variable into `agents.default` once, at config load. This process's value
wins when it has one and the login shell's answers otherwise, so the New Session
form preselects the agent hive would run rather than the one a launcher
environment resolved (`SessionsService.SessionLaunchOptions`).

### App modes

The desktop shell has a fixed set of top-level modes — today Hub, Terminal,
and Agents — that `App.vue` owns as a closed type (`'hub' | 'terminal' |
'agents'`) rather than a boolean per mode. Each mode's `*Active` computed is a
sibling of the others (`mode.value === '<name>' && shellLoaded.value &&
!onboardingActive.value`), never an `else` branch of a two-way toggle — a
third mode written as `!terminalActive` would render underneath whichever
mode it collided with. `TitleBar` carries the same closed union for its
switch, and `router.ts`'s routes each render a null `ShellPage` component
because `App.vue`, not the router, owns what is on screen. A mode remembers
the last route it was on (`lastTerminalPath`, `lastAgentsPath`) so returning
to it resumes rather than resetting, and ships dark behind its own
`experimental.*` opt-in, gated independently of the others (ADR terminal-experimental-gate is the
mechanism; ADR a-workspace-declares-its-own-authority is the Agents area's adoption of it).

### Terminal sessions

Terminal mode attaches one tmux control-mode client per Hive session, keyed by
the session **slug** (the tmux session name). Four pieces, and the split between
them is the constraint (ADR terminal-transport):

- **`internal/app/tmuxcc`** — the protocol: line framer, `%begin`/`%end`/`%error`
  command FIFO, notification dispatch, `%output` octal decode, one client per
  slug over `tmux -C attach`, and a per-session fan-out broker. **No transport
  and no UI** — it is driven over injectable process pipes, so it is testable
  without tmux, HTTP or Wails. It holds no environment *policy* either, but it
  does take an environment: `ManagerOptions.Environ` is wired to `execenv` at
  the composition root and is what every tmux it execs runs with (ADR tmux-runs-in-the-resolved-environment) —
  see [Subprocess environment](#subprocess-environment).
  `Manager` owns an app-lifetime context and joins
  the App-owned lifecycle behind a `stopOnce` (PR rule 8), and also runs the
  one-shot commands that keep a slug and its tmux session in step
  (`RenameSession`) — through the same `$TMUX` socket resolution an attach uses,
  or they would address a different server; tmux is spawned
  outside any request context, so an `Attach`'s context bounds only its
  handshake, and processes are killed explicitly on teardown.
- **`app.TerminalsService`** — the slug-keyed driving service both adapters
  call. It pre-validates (unknown slug, window id not in the client's window
  set, size and name bounds) so a `Kind` is chosen without matching error text,
  and it holds **no** token, base URL or stream path: the core stays
  transport-neutral. **Attach never spawns; `Start` and `Kill` are the terminal's
  lifecycle** (ADR terminal-start-is-an-offered-action) — spawning runs the session's agent command, and the
  view attaches without a click. A slug tmux is not running is a `KindNotFound`
  decided by a `has-session` probe, never by reading a dead control stream's
  message, and the frontend turns it into a panel offering to start. What a
  started session *contains* is hive's spawn configuration, reached through the
  seam (`SessionsService.StartTmuxSession` → `HiveSessionManager.SpawnTmuxSession`
  → hive's `OpenTmuxSession`, detached); a kill is tmux's alone and touches no
  hive record, which is what separates it from delete and recycle. Anything that
  builds a session's windows here instead has to revisit that ADR.
- **`internal/adapter/httpapi`** — the control plane as errchain operations
  (`POST /api/terminal/…`) in the operations table, and the data plane as a raw
  WebSocket handler at its own prefix. See
  [Data-plane mount](#named-patterns).
- **`adapter/wailsui.TerminalService`** — `Enabled`, `Available` and `Endpoint`,
  the frontend's only gate and bootstrap. `Enabled` reports the
  `experimental.terminal` opt-in (ADR terminal-experimental-gate) — off means the Hub|Terminal toggle
  never renders. `Available` must answer while the loopback
  server is down, so it composes tmux/build/platform availability with loopback
  reachability; `Endpoint` builds `{httpBaseURL, wsURL}` from the live bind plus
  the token it was handed.

**A first paint is a pane's scrollback, its screen at exactly the window's
height, and its cursor** (ADR terminal-first-paint-carries-scrollback) — three tmux commands per pane, replayed
as one byte stream into a fresh emulator. **An attach paints the active window
and answers; the rest are painted straight after on the client's own lifetime**
(ADR attach-paints-the-active-window-first). `capture-pane -e` is what a first paint costs — 7.5x the same
capture without escape reconstruction, linear in scrollback depth — so painting
every window before answering made attach latency scale with a session's window
count for panes the renderer could not show. A deferred pane is held from
before the synchronous paint begins, `openAll` leaves a held pane's buffer
alone, and a repaint waits for an in-flight deferred pass rather than
interleaving with it. Two invariants hold it together and
both are easy to break by accident. The screen must be written at the full
window height, because an emulator pins its viewport to the last rows it was
written and a short screen seats the pane's row 0 partway down it — every
cursor-addressed redraw an alternate-screen program makes then lands that many
rows off. And the cursor must be read *after* the paint gate's mark, because
output produced between the read and the captures is corrected by the replay
that follows, while output produced before the mark is discarded and never is.
History is bounded at tmux's own default `history-limit` so an unconfigured
tmux replays all of it and a configured one cannot make attach cost unbounded.
`-J` joins wrapped rows in the history only: a joined screen row re-wraps into
more rows than it was captured from and breaks the height.

**Every attach leaves a first paint on the stream, including one onto a client
that is already live.** A transport-only drop — a stalled write, a reloaded
webview — takes the emulator and leaves the control client attached, and what
that stream already delivered is not in the broker's backlog to replay, so an
attach that answered from memory would hand a fresh emulator a session it can
only render the future of. The repaint resets the broker first: the snapshot
supersedes every undelivered byte, and a subscriber still draining the dropped
stream would otherwise consume the snapshot meant for its replacement. The size
vote is a fresh attach's alone — a live client keeps the one it already cast.

**What a subscriber is owed is a byte stream, not the frames it was published
in.** tmux caps a `%output` at 2KB, so a busy pane produces ~14.5k events a
second and every fixed per-frame cost downstream is multiplied by that. The
broker folds an incoming output into the last queued event for the same pane
(ADR queued-terminal-output-coalesces). It costs no latency because a merge is only possible when a second
event is already waiting — a consumer keeping up sees every event whole — and
it never merges into the head, which `pump` may already be delivering. Byte
accounting is unchanged, so the overflow bound trips at the same volume.

The frontend holds a small LRU pool of live attaches rather than one:
switching sessions hides the outgoing panes instead of detaching, and a cold
attach keeps the outgoing screen until the incoming one has painted, so a
switch never blanks the pane (ADR terminal-attach-pool). Detach fires on eviction, explicit
close, the session leaving the listing, and view unmount — not on switch.

**Terminal mode is mounted on first entry and hidden, never unmounted, on the
way back to the hub** (ADR terminal-mode-is-hidden-not-unmounted) — the same rule the pop-up panel follows, and
for the same reason: an unmount is what makes the pool pay for itself again.
The mode takes an `active` prop, and that, not mount, is what starts the
status poll and the window-listing sweep and what revalidates the session tree.
Anything the mode must not do off-screen belongs on that prop; anything it must
keep across a trip to the hub can now simply live in the component.

**Terminal style is a setting, not pane chrome.** Text size, family, weights,
line height and tracking are all `appearance.terminal_*` settings written from
Settings ▸ Terminal, and the pane carries no duplicate control for
any of them (ADR the-sidebar-tree-is-the-only-window-list). A new style option takes the same route. The chrome's own
faces are a separate pair of settings on a separate pane — `appearance.
font_family` and `mono_font_family`, written from Settings ▸ Appearance — and a
terminal takes nothing from them (ADR the-app-s-faces-are-picked-from-installed-fonts-not-from-a-bundled-set).

**The sidebar tree is a session's only window list.** There is no tab strip to
keep in step with it, and a window's controls — close, rename, and the add on
its session's row — live on the rows themselves (ADR the-sidebar-tree-is-the-only-window-list).

**One tmux session in the tree belongs to no hive session: the scratch
terminal** (ADR the-scratch-terminal-is-a-tmux-session-the-desktop-owns). It is an ordinary tmux session under a reserved slug —
`Scratch`, which hive's `Slugify` cannot mint because it lowercases before it
replaces — so attach, the sweep, tabs, reorder, kill and the pool serve it
unchanged, and the desktop owns only what creating it means: one session whose
*working directory* is the user's home, which is therefore every tab's.
`TerminalsService.Start` is where that branch lives, and `Scratch()` is the
declaration the tree pins its section from. In the tree it is a group of one
that draws its heading — Terminals — and its tabs where a repository draws its
own heading and its sessions: the session row itself is not rendered, because a
group that holds one session forever buys a level of nesting and names the same
thing twice, so `Scratch` is a tmux name rather than anything on screen. Four
rules follow from it having no hive record: it is created with `tmuxcc`'s own
empty-command create, so its shell is an interactive login shell and the user's
startup files are what put their tools on its PATH — over the resolved
environment underneath, which an interactive shell has no need of (ADR subprocess-environment,
ADR tmux-runs-in-the-resolved-environment); its liveness comes from the window sweep
rather than from `SessionStatuses`, which is keyed by session id, and its tabs
are listed whatever `terminal_show_windows` says because they *are* the section;
`+` on its heading creates the session when tmux is holding none, which is
scratch-only because starting a hive session runs its agent; and rename,
recycle, delete, session details and configured actions are not offered, because
each addresses a record that does not exist. Prune is untouched — it acts on hive's listing, which the
scratch terminal is not in.

**The other tmux sessions in the tree belong to no hive session either: pinned
agent chats** (ADR a-pinned-agent-chat-is-attached-by-the-code-view-as-an-ordinary-tmux-slug). A chat is already a tmux session on this same control
plane (ADR agent-workspace-sessions-are-tmux-sessions), so pinning one adds a `Chats` section above the scratch
terminal's and its rows attach through the same pool — `useTerminalPinnedChats`
turns the pin set into `TerminalSessionRow`s keyed on `SessionView.Slug`, which
the core declares whether or not the chat is running so a stopped row still has a
pool and route key. It borrows the scratch terminal's two exemptions — liveness
off the window sweep, swept whatever `terminal_show_windows` says — and differs
in three ways: the row is a **leaf** (a chat is one conversation, so its tmux
window is not something to navigate between), starting it is
`AgentWorkspacesService.ResumeSession` rather than `TerminalsService.Start`
(there is no hive spawn configuration behind an `agentws-*` slug), and its row
menu offers only the pin's own two entries, because the Agents area owns a
chat's rename, stop and delete. The pin set is `localStorage` — an arrangement of
this sidebar, not a fact about the chat — which is also why
`TerminalSessionGroup` now carries a closed `kind` union (`chats` | `scratch` |
`repo`) instead of a `pinned` flag that had been standing for two different
things.

**Adding a window does not require an attach.** A slug with no control client
gets one from a one-shot, whose `-c` is spelled `#{session_path}`: tmux resolves
an unset start-directory against the client running the command, and a one-shot
command client is this process, so the window would otherwise open in the app's
working directory rather than the session's.

**The sidebar filter narrows what the tree draws and nothing else.** The
attachable set still carries every session, because the watcher that follows a
rename or a deletion reads it and a session filtered off the screen must not
read as one that went away; the attached session is not exempt from the filter
either, and stays on screen while its row is hidden. The tree's keyboard walk
reads the filtered groups, so an arrow only ever lands on a row that is drawn.
`terminal.focus-filter` (`/`) is an ordinary catalog command, so a focused pane
keeps the key — a bare `/` is a character, and the tree is where a search for a
session starts.

**`terminal.select-window-1` … `-9` name a position in that list, not a tmux
index.** The list is what is on screen and tmux's indices have gaps as soon as a
window is closed, so `⌘3` is the third row rather than window 3. A session with
fewer windows ignores the chord instead of clamping to the last: the chord means
one window, not whichever is nearest.

**The window lifecycle is four bindable commands, not handlers on the mode.**
`terminal.new-window`, `terminal.close-window`, `terminal.next-window` and
`terminal.prev-window` act on the attached session and its active window,
dispatched from App.vue through `lib/terminalTree`'s handles like every other
`terminal.*` command — so they answer from the session tree and from a focused
pane alike, and rebinding them is an ordinary settings change. Two things they
do not share with the numbered jumps: they **escape** a pane rather than pierce
it (see Pop-up terminals), and next/prev **wrap**, where the tree's own walk
clamps — a session's windows are a ring in every terminal emulator, and there is
nowhere else for "next" to go from the last one. A window this view created
takes focus when tmux announces it; one another client opened does not, because
it must not pull the keyboard out of the pane in front of the user.

**Window order is tmux's, and a reorder is a move rather than a swap.**
`POST /api/terminal/windows/move` takes a window id and the index it ends up at,
because a reorder means a destination and not a neighbour; the core reads the
current order and picks the tmux insertion that expresses it, anchored on a
window id since the move renumbers the very indices an anchor would be read
from. Three rules hold it together and each answers something tmux does:

- **The answer is a window set, not an acknowledgement.** The order is session
  state every attached client shares, so the move replies with what tmux settled
  on and the strip renders that rather than the order it asked for. The tab
  strip and the sidebar's window well are two views of that one order and either
  can drag it, so neither holds an arrangement of its own.
- **A move is an unlink and a relink**, so tmux announces the window it moved as
  closed. `tmuxcc` suppresses that close for the window in flight — acting on it
  tears the tab and its terminal down, and the reconcile behind it can only
  rebuild them blank.
- **`-d` follows the moved window.** tmux selects whatever it moves, and `-d` on
  the window that is already selected deselects it, so the flag is passed exactly
  when the moved window is not the active one. Either flag fixed loses the
  selection in one of the two cases, and a reorder is not a selection.

Indices are renumbered after the insert (`move-window -r`), because an insert
leaves a hole where the window came from and an index is what tmux's own key
bindings and every other attached client address a window by. The controller's
window order comes from `list-windows` and nothing else: a reorder changes no
window, so a set that keeps discovery order would never notice one.

Live terminal status is a separate pull projection from session lifecycle
state: `SessionSummary.State` remains active/recycled/corrupted, while
`SessionsService.SessionStatuses` reports whether each tmux session is running
and active/approval/ready/missing agent activity keyed by stable tmux window id.
The session row renders liveness; activity belongs to the window row. Terminal
mode polls that projection only while mounted, at Hive's configured tmux
interval. The Hive anti-corruption layer drops captured pane content and
provider errors before the Wails boundary, and its status integration runs
through `tmuxcc.Commander`, so detection uses the same resolved binary,
environment, and tmux server socket as control-mode attaches.

The slug is load-bearing in both products, so **`session.Slug` must equal the
live tmux session name**, and the desktop is what keeps it that way. Hive's
`RenameSession` re-slugs the record and leaves tmux alone, so
`app.SessionsService.RenameSession` renames the tmux session first, writes the
record second, and rolls the tmux rename back if that write fails — with a slug
collision rejected up front, because hive checks none and the table has no
uniqueness constraint. ADR session-rename-keeps-slug-and-tmux-in-step. A change that gives the slug a second identity,
or that makes something else the attach target, has to revisit that ADR rather
than work around it.

Session lifecycle (read, rename, delete, recycle, prune, and spawning the tmux
session a slug names) reaches hive through `dispatch.HiveSessionManager`, a
second seam type beside `HiveSessionLauncher`: launching is a dispatch action an
output command holds, and it has no business holding a delete. Delete, recycle and prune run through
`jobs.Track` like `CreateSession` does — they do git and worktree work — so
`jobs:updated` is what refreshes the list, and the frontend follows a session by
**id** across a reload so a rename is told apart from a deletion. Anything
destructive is gated on `SessionRisk`, whose payload names the uncommitted or
unpushed work at stake and whether recycling this session is really a delete (it
is, for a worktree session).

**A session this app created for an inbox item stays findable from that item**
(ADR an-item-session-link-is-desktop-state-keyed-on-item-coordinates). The association is `item_session` in `desktop-pipeline.db`, keyed by
hive's session id — never written into hive's own record, whose model must not
learn what an inbox item is. Four rules are load-bearing:

- **The item is named by its coordinates** (`profile_id`, `source_kind`,
  `source_scope`, `external_id`), not by `inbox_item.id`, and the table has no
  foreign key. A replay deletes and rebuilds every row a profile owns, so a row
  id would drop the associations on an ordinary flow edit. Anything that
  rewrites those columns has to move the links with them —
  `resolveInboxItemScoped`'s scope heal does, and `PurgeProfile` drops them.
- **Nothing about the session is mirrored.** `SessionsService.ItemSessions`
  reads name, slug, repo, state and liveness from hive on every call, so a
  session renamed outside the app cannot be shown under its old name.
- **The read is what reconciles.** A link hive cannot account for is dropped —
  but only behind a *successful* listing, because a failed one is not evidence a
  session is gone.
- **The item an action ran against travels on the `output_command` row.** Its
  dedup key is the occurrence key, which names no item, so `actionSinks` carries
  the source identity the way `notifySinks` already does and the enqueue records
  it. `OutputData.Origin` is how it reaches an executor; a launcher records the
  link and never fails the launch over it.

**The user's own operations on a session are `actions.yml` entries, not a second
config** (ADR actions-target-terminal-sessions-and-windows). An action declares its surfaces in `targets:` — `item`
(the default, and what every pre-terminal action means), `session`, `window` —
and `SessionsService` owns the three methods behind them: `TerminalActionViews`,
`InvokeTerminalAction`, `RenderTerminalClipboardAction`. Three rules are
load-bearing:

- **The caller sends identity, the core resolves the rest.**
  `dispatch.TerminalTarget` is a slug and an optional tmux window id;
  `.Session.Path`, `.Session.Repo` and the rest come from the session record at
  invocation time, so nothing lets a client choose the path a command runs in.
  `OutputData.Session`/`.Window` are nil on the item path, which is what makes a
  template reading the wrong surface fail rather than render blank.
- **A terminal action enqueues no `output_command`.** The durable command's
  `UNIQUE (action_id, key)` exists to stop an already-run action from re-firing,
  and a manual operation against live local state must stay repeatable and must
  not replay after a restart. It runs through the same `Dispatcher` — now built
  by `App` and shared with the output worker — inside `jobs.Track`, and the tail
  of stderr goes into the job's failure reason because there is no durable row
  holding its streams.
- **`launch-session` is refused on a terminal target**, in `validateActions` via
  `TerminalCapable`, so the refusal lands when the catalog is authored.

**Which tmux runs is `internal/app/tmuxbin`'s answer, not `$PATH`'s** (ADR
0039). A desktop launch inherits no shell `$PATH`, so the resolver checks
`paths.tmux`, then `$PATH`, then the prefixes package managers install
into, and remembers only success — installing tmux does not need a relaunch.
`tmuxcc` holds none of that policy: it takes a `func() (string, error)` and the
resolved path travels on `Options.Binary`. Hive session spawning execs tmux from
vendored code, so it gets the same binary through `app.tmuxExecutor`, a
decorator over `executil.Executor` that substitutes the command name `tmux`.

The whole surface ships dark behind `experimental.terminal` (ADR terminal-experimental-gate): when
off, `main.go` mints no token and neither the control-plane routes nor the
stream mount exist. The bearer token is minted per run in `desktop/main.go` and
passed to the two
adapters that need it, so no core type carries a transport credential. Terminal
availability is gated on `http.enabled` — no loopback server, no terminal — and
that, a missing tmux, tmux `< 3.2`, and the `-tags server` build all surface
through the same unavailable-with-a-reason state.

The reader goroutine always drains tmux's stdout, because command replies share
that pipe with notifications; notification dispatch and broker publish are
therefore non-blocking. The broker's per-session buffer is bounded **by bytes
and by event count** — only output is worth bytes, so the count is what bounds a
backlog of window events. There is no drop-oldest (it corrupts emulator state)
and no tmux `pause-after`. Both bounds admit **only droppable events**, and only
a droppable event may trip one: a lifecycle event that tripped the bound would
be discarded by the same branch that drops for overflow, leaving the stream with
nothing saying what happened to it.

**Crossing the bound degrades the stream rather than ending it** (ADR terminal-overflow-resyncs-the-view-instead-of-ending-the-stream).
The broker's `resync` drops the backlog and lifts the overflow state *keeping
the subscriber* — the difference from `reset`, which exists to hand the snapshot
to a replacement — a `degraded` lifecycle frame says output was lost, and the
same `paintEveryWindow` an attach and a repaint run puts every window back on
screen. Reusing that path is what makes the recovery non-lossy: the snapshot
carries the pane's scrollback, so the flood the user wants to read survives the
bytes that carried it. The frontend resets each emulator on the frame — the
snapshot's own scrollback would otherwise replay lines the pane is already
showing — and raises a dismissible notice, because a recovery that hides the gap
is worse than the teardown it replaced. **The fatal path is still there** for a
resync already in flight, a resync inside its cooldown, or a repaint that
errors: those end the stream with `EXITED(overflow)` and the frontend
re-attaches, exactly as before.

**Size is a negotiation this app is only one voice in.** Every client attached
to a session renders the same grid per window, and tmux's `window-size` option
decides whose size that is — by default the most recently used client's — so a
resize is a vote and the window event is the answer. Two rules follow, and both
are load-bearing because a vote reaches every other client attached to that
session:

- **Never vote a size nothing measured.** Attach carries the last size this app
  window measured, or `0x0`, which sets no client size at all — tmux ignores a
  control client until it sets one, so the session keeps the size its other
  clients gave it. A placeholder would resize the session, and the agent
  redrawing inside it, before anything had been measured.
- **A vote tmux does not grant is reported, not retried.** The frontend
  compares its vote against the granted size and names the constraint; nothing
  re-votes to win the size back from the other client.

On the frontend, a pane on screen **always renders through an atlas renderer** —
WebGL, falling back to 2D canvas — because only an atlas renderer strokes box
drawing and underlines to the cell's device-pixel bounds; xterm's DOM renderer
cannot join either across cells at any size or device pixel ratio. Four rules
follow and are the ones to keep (ADRs terminal-atlas-renderer, terminal-renderer-claimed-on-activation): the renderer is claimed after
`term.open()` and never before, and **when the window is first shown rather than
when its pane mounts** — the pool mounts a pane per window of every attached
session, and claiming at mount spends a GL context per background tab and walks
the page past WebKit's per-page limit, where the context it costs is another
session's pane; a pane that ends up on the DOM renderer is a logged degradation
path, not a supported one, and a failed canvas claim is recorded so the next
activation retries rather than stranding the pane there; **do not set
`lineHeight` or `letterSpacing`** — every renderer quantises both to whole
device pixels, so neither can tune a cell onto a cleaner boundary and a
`lineHeight` above 1 pads the glyph off the edge box drawing has to reach; and
the addon majors are pinned to the xterm core major, since they reach into
`Terminal._core` for private services.

#### Pop-up terminals

`internal/app/ptyterm` is the *other* terminal backend, and the rule for which
one serves a request is the session: **a terminal that belongs to a hive
session, or to an agent workspace's own durable session record, is tmux's; a
terminal that belongs to a moment is this one's** (ADR ephemeral-popup-terminals; ADR agent-workspace-sessions-are-tmux-sessions moved
agent workspace sessions onto tmux, so the pop-up is `ptyterm`'s only caller
now). It owns a PTY and the process on the far end directly — no multiplexer,
no discovered binary, no negotiation — and every terminal it opens dies with
the app.

Three rules govern it, and each is a consequence of that:

- **A terminal is addressed by an id, and minting one is the default rather
  than the rule.** `Open` mints an id (never a slug) when the caller supplies
  none — the pop-up always takes this path, since nothing about it wants to be
  addressable. A caller that already knows the id it wants to reattach to may
  supply its own instead (`Spec.ID`); a caller-supplied id collides with
  `ErrIDInUse`, not a second terminal (ADR ptyterm-terminals-are-caller-addressed) — no caller currently
  exercises this since the pop-up is the one caller left and always mints.
- **The manager caps concurrent terminals at `maxConcurrentSessions` (8).**
  The terminal past the cap returns `ErrTooManyTerminals` and spawns no
  process, and closing one makes room for the next (ADR ptyterm-terminals-are-caller-addressed). Agent workspace
  sessions have their own, separate cap now — a count of live `agentws-*` tmux
  sessions (ADR agent-workspace-sessions-are-tmux-sessions) — since they are no longer this manager's terminals.
- **A launch is a directory and a shell command line.** The directory resolves
  launcher cwd → session checkout → explicit path → home; the command runs
  through a login shell so the user's own aliases resolve it, over `execenv`'s
  resolved environment as the floor rather than instead of it (ADR subprocess-environment, ADR
  0068), and empty means an interactive shell. A named launcher is that spec
  with config in front of it — add the config, not another launch path.
  **The home fallback is the bare shell's alone.** A launcher with no configured
  `cwd` is session-scoped: the core refuses it without a slug (`KindInvalid`),
  drops any `Dir` sent beside one, and answers a slug whose session is gone with
  `KindNotFound` rather than opening somewhere else (ADR quick-terminal-launchers-are-session-scoped). That is in
  `PopupTerminalsService`, so an HTTP API caller is bound by it too.
- **A launcher is an entry in actions.yml's `launchers:` list, opened by id**
  (ADR launchers-are-their-own-list-in-actions-yml). It is deliberately *not* an action: every surface in the `targets`
  vocabulary dispatches and a pop-up does not, so it shares the file — one
  loader, one watcher, one last-good reload — and none of the action envelope.
  Its `command` and `cwd` are used as written rather than rendered, because a
  login shell in the working directory is the only context a launcher needs.
  Each one is a bindable command, `launcher.<id>`, unbound by default; a launch
  that differs from the live one replaces it, since one pop-up is open at a
  time. `PopupLauncher.requiresSession` — a launcher with no `cwd` — is the
  core's answer to where it may run, and the frontend takes the command's
  context from it (`terminal-session`) rather than re-deriving one, so the
  keymap and the palette gate on what the core enforces (ADR quick-terminal-launchers-are-session-scoped).
- **The PTY is spawned at the grid the pane measured.** The panel is built and
  measured first, and the launch carries its cols/rows; the server applies them,
  because one client renders this PTY and a measurement here is the size rather
  than a vote. Sizing it afterwards is what a TUI sees as a full screen at the
  fallback grid followed by a SIGWINCH reflow — the pop-up appearing small and
  snapping wider. A pane with no box to measure sends nothing and takes the
  server's default; it must never send a placeholder.
- **`/api/terminal/popup/…` is its own path space under the terminal prefix**,
  covered by that prefix's bearer token and CORS policy for the same reason
  (ADR terminal-transport): a pop-up spawns a shell, which is arbitrary command execution.
  Its data plane is `/api/terminal/pty/stream`, carrying one terminal per
  socket, so its frames carry no window or pane ids and are not the tmux
  stream's. `/api/terminal/agents/…` sits under the same prefix for the same
  reason but is control-plane only since ADR agent-workspace-sessions-are-tmux-sessions: an agent workspace
  session's data plane is the tmux stream (`/api/terminal/stream`), the same
  one a hive session's terminal rides, addressed by the tmux session name
  `AgentWorkspacesService` gives it rather than a hive slug.
- **Hiding the panel keeps the shell; exiting the shell takes the panel.** The
  toggle opens a terminal outright, returns to a running one, and hands focus
  back where it came from on the way out. Anything that adds a step between the
  shortcut and a prompt is working against what this is for.
- **A focused pane keeps every key it can use, and a short list gets one back**
  — in two ways, which is the distinction to get right before adding to it.

  A command **pierces** when it is claimed on the binding alone: the pop-up
  toggle and any launcher chord, because the combo that opens one has to close
  it — a launcher only where its own context is active, so a session-scoped one
  leaves the key to the pane outside a session (ADR quick-terminal-launchers-are-session-scoped);
  `terminal.focus-sidebar`, because it is the way back to the session tree;
  and `terminal.select-window-1` … `-9`, because a jump between windows is only
  ever wanted from inside the one being left. Where `mod` is Ctrl these take a
  readline chord away from the pane, which is the price of a chord that has to
  work from inside one.

  A command **escapes** when the catalog marks it `escapesPane`: it is claimed
  through `terminalEscapeCombo`, which takes Command chords and Ctrl+Shift where
  there is no Command, dropping that Shift so one configured combo matches on
  both. The palette is one, because it is the way back out of a pane, and so is
  the window lifecycle — `terminal.new-window`, `-close-window`, `-next-window`,
  `-prev-window`. That is what leaves a bare Ctrl+K as readline's
  kill-to-end-of-line and Ctrl+T as its transpose-chars while ⌘K and ⌘T are the
  app's. Prefer escaping: piercing is for a chord the escape form cannot carry.
  Anything added either way has to answer why a pane may not have the key.

  Both are two-sided: the dispatcher must act on the chord *and* xterm's
  `attachCustomKeyEventHandler` must decline it, or the pane writes it to tmux
  as well. Both sides resolve through the live keymap, so a rebind moves them
  together.
- **A pane takes focus for the mouse, not for the arrows.** `paneMayAutoFocus`
  gates the automatic `term.focus()` calls — the ones on attach, on reveal, and
  on a window switch — because walking the session tree past a session is not an
  intent to type in it, and a pane that grabbed focus mid-walk would send the
  next arrow to tmux. Explicit requests (Enter, the focus chord, the scroll
  pill) call `focusActive()` directly and do not consult it. The latch records
  the last input to move the selection rather than bracketing one activation:
  the calls it gates are spread across an attach that may not paint for a
  second.
- **Widget-local keys are not catalog commands.** A combo resolves to exactly
  one command — the first in the catalog claiming it — so `context` narrows when
  a command fires but does not let two commands share a chord. The session
  tree's `↑`/`↓`/`j`/`k` would have had to fight the feed's `j`/`k` for the same
  combo, so they are handlers on the widget that owns focus, as the command
  palette's own arrows already were.
- **The panel's box is derived from the window, never stored.** Centred, a fixed
  fraction of it, following a resize; not draggable and not resizable for now.
  Restoring either means answering how a remembered box stays honest against a
  window that changed since — a stored one that does not is a bug that presents
  as the pop-up ignoring its own setting.

Do not build a shared interface across the two backends, and do not extend one
because the other has something: they answer different questions, and the
overlap in vocabulary is a coincidence of both being terminals.

### Agent workspaces

`internal/app/agentws` owns the on-disk agent-workspace root
(`settings.Paths.AgentWorkspacesDir`, default `<ConfigDir>/workspaces`):
`mcps.yaml`, `.shared/skills/`, and one directory per workspace. Every
directory splits **authored** files a user (or an agent, via the
`hive-agent-workspaces` skill) writes — `agent-workspace.yaml`, `AGENTS.md`,
`docs/` — from **generated** ones `agentws.Generate` produces on every open —
`CLAUDE.md`, `.mcp.json`, `.codex/config.toml`, `.claude/skills/`,
`.agents/skills/`, an empty `docs/` seed. ADR workspace-directories-are-generated-and-disposable is the contract behind that
split: generated output is disposable, never drift-tracked, and a hand edit to
it is silently replaced on the next open — deliberately cheaper than the
skill installer's hash-tracked model (ADR skill-installer), because Hive owns this whole
subtree. `Generate` **reconciles** each of its owned trees to exactly what it
computes rather than clearing and rewriting: a file no longer in the target
set is removed, an emptied directory is pruned, and a file already present is
written only when its bytes differ — the write-only-if-different rule is what
makes byte-determinism observable (nothing to re-sync when nothing changed)
and what makes concurrent generation from two machines safe (ADR workspace-directories-are-generated-and-disposable).

A workspace created through the app also starts with an `AGENTS.md`
scaffold — authored at birth, written exactly once, never regenerated
(ADR a-created-workspace-starts-with-an-agents-md-scaffold): overriding the default framing is editing the file, and deleting
it deletes it. Hand-authored workspaces get no scaffold.

The app writes authored YAML only through the node-tree editors in `write.go`
and `librarywrite.go` — parse, edit in place, re-encode — so comments, key
order, and keys the writer does not own survive; `yaml.Marshal` is never the
writer. The workspace editor owns `name`, `agent`, `autonomy`, and `mcps:` in
the manifest (an empty list removes the key); `skills:` and everything else
stay the user's. `mcps.yaml` gains entries through the same pattern —
`ParseMCPImport` accepts pasted MCP JSON (claude's `mcpServers` wrapper or a
bare id-to-server map), an id already declared is a conflict rather than an
overwrite, and only user entries can be removed. The merged catalogue
(shipped + user, stability, the resolved command line, a LookPath problem) is
served on the agents API — the surface ADR a-workspace-declares-its-own-authority §5's read-the-command-first
mitigation runs through.

Two directory actions ride the same token-guarded agents prefix, because
launching a program is command execution (ADR terminal-transport): open-in-editor runs the
settings-configured editor (`editor.command`, a single word — the agent-command
rule from ADR a-workspace-declares-its-own-authority — resolved and launched through `execenv`, ADR subprocess-environment) on a
recognized workspace's directory, and reveal opens it in the OS file manager.
Both refuse a path that is not a known workspace, the same posture as
`SystemService.checkAllowed`.

A workspace declares `autonomy: ask | auto | full`, omitted defaulting to
`ask` since the M2 approval indicator makes it legible (hc-ou4o02zx §4) — and
`agentws`'s launch table (`launch.go`) maps
`(agent, autonomy)` to that agent's own CLI flags; an agent or posture with no
table entry fails closed (`ErrUnknownAgent`, `ErrNoAutonomyMapping`). The
table is also projected to the UI (`AutonomyFlags`): the editor's posture
selector lays out every option with the exact flags it launches for the
chosen agent, so `full` reads as the dangerous bypass it is, and a posture
the launch would refuse is disabled rather than hidden. Those
flags come from nowhere else: hive's own `AgentProfile.Flags` are dropped at
the vendored seam (`agentCommands` in `app.go`) before they ever reach a
workspace, and only `Command` crosses — validated as a single shell word, so a
flag cannot re-enter through the command string either (ADR a-workspace-declares-its-own-authority).

Each agent's MCP wiring (`MCPWiring`) is data, not a branch: a `File` the
generator writes, a `Render` encoding the resolved servers into that file's
format, and a `Bounded` flag reporting whether the wiring confines the agent
to exactly the workspace's declared set. Claude's is bounded
(`--strict-mcp-config` plus a generated `.mcp.json`); codex's is not — it has
no CLI-level MCP flag, so its generated `.codex/config.toml` is loaded
alongside whatever the user's own global codex config already has, and the UI
states that rather than leaving an unbounded tool set looking identical to a
bounded one.

`agentws.Watcher` follows the tree's own shape rather than `ActionsWatcher`'s
or `FlowsWatcher`'s flat one: fsnotify is not recursive and the tree is
nested, so it maintains a watch at two levels — one on the root itself (which
sees `mcps.yaml` and workspace directories appearing or disappearing) and one
per workspace directory (which sees its `agent-workspace.yaml`). Nothing
watches deeper: an agent writing into `docs/`, or the generator rewriting its
own output on open, is invisible to it by design, not omission.

## Execution model

The flow engine runs **in Go**, in-process. Source polling, graph routing,
script evaluation, and action execution all happen in the core; the frontend
owns the editor, not the runtime.

This replaces a split model in which Go ingested and executed while the
browser routed. That split made the desktop window a hard dependency of flow
execution and put the correctness-critical parts — topological order,
fan-out, offset advancement, commit atomicity — out of reach of any headless
surface. Consolidating in Go is what made the dry-run surface possible: one
execution path an editor preview and the `execute_flow` MCP tool each drive
identically, rather than a second implementation of routing semantics.

`Runner.Run` returns the `CommitBatch` a batch of messages is worth and
**does not commit it**. Reading the log and applying the batch belong to the
caller, which is what makes a dry run and a live tick one code path.

`Runner.DryRun` is the second entry into that path: it delivers messages to a
node the caller names — any node, not only an entry — and returns a per-node
trace instead of a `CommitBatch`, so nothing it produces can be applied. What
each node received, what it emitted per output port, its drops, timing,
console output and structured script errors come back; the outputs and KV
mutations a live run would have committed come back beside them. `FlowsService`
builds a throwaway `Runner` over a `MemoryKV` for each call, because a `Runner`
carries function-node `state` between messages and the engine's installed one
must not be mutated by a debugging call. ADR flows-are-dry-run-against-supplied-input.

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
or accounting still belongs in one. ADR flow-engine-in-go.

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
- **`console` writes to a sink, not a log.** It is bound in every VM so a
  script that logs is never a script that fails, and each call goes to the
  invocation's `ConsoleSink`. A live run supplies none, so the calls go
  nowhere; a dry run collects them per node.
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
ADR goja-script-runtime.

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
   before cutover. The largest step. **Done.** ADRs goja-script-runtime and flow-engine-in-go. The shared
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
5. **Source registry** — **Done.** ADR source-connector-registry. `connector.Descriptor` declares a
   connector and `connector.Factory` constructs it; capabilities are read off
   the instance rather than type-asserted; `flow`'s and `runtime`'s registries
   derive their source entries. Node types are namespaced (`sources.github`,
   `sources.webhook`), which breaks the `type:` discriminator in any existing
   `flows/*.yaml`.
6. **Credentials** — **Done.** ADR credential-store. `Ref{Provider, Account}`, the
   keychain-backed store and its ref index, one fetcher per account, and
   GitHub demoted from a login to a connector. A source node's `credential:`
   is required, which breaks any existing `flows/*.yaml` a second time.
   First run creates the workspace before it offers to connect anything, and
   `flow` no longer names a connector: `FlowStore.Create` takes its starter
   graph from its caller.
7. **Adapters** — HTTP and MCP mounted in-process; plugs for lifecycle
   (the lifecycle half is blocked on appkit — see
   [Background lifecycle](#background-lifecycle)). **Done** for the agent
   surface: `mcpsrv` serves tools over `App` at `/mcp` on the shared loopback
   server (ADR mcp-replaces-the-agent-facing-http-api), and the REST control surface it replaced is deleted.
   `httpapi` remains for the frontend's terminal transport and the liveness
   probe. The full REST + SSE *product* surface is still to come, as is the
   plugs lifecycle.

### Data that must survive

Breaking changes to DB schema are acceptable and carried forward by the
table-tracked SQLite migration runner. Breaking changes to config **format**
(`settings.yaml`, `flows/*.yaml`, `actions.yml`) are carried forward the same
way, but by the separate per-file migration runner (ADR yaml-config-migration) rather than by
breaking. Three things are not rebuildable and must be carried across any
migration:

- `inbox_item.unread` / `archived_at` / `archived_actor` / `archived_reason` —
  triage decisions, not derivable from any source.
- `output_command`'s unique `(action_id, key)` index — the only thing
  preventing an already-run action from re-firing.
- `node_kv` — a function node's dedup/change-detection memory. Replay is
  deliberately KV-inert, so a lost seen-set cannot be recomputed; dropping
  it re-fires every notify-once branch.
- `item_session` — which hive sessions an item started. The session survives
  independently and carries no back-reference this app reads, so a dropped link
  cannot be rebuilt from either side.

Everything else (`event_log`, `feed_membership_claim`, `node_run`,
`source_head`, `consumer_offset`, `activity_event`, `job`) is derived or
replayable. Migrations are not squashed, because a squashed `0001` would not
match existing `schema_migrations` rows. Once a migration appears in a
`desktop-v*` release, its filename and contents are immutable; later migrations
append contiguous versions above it. `scripts/check-migration-order.sh` compares
the tree with the latest reachable desktop release tag, and both the normal
quality gate and the release CLI run it.

## Open questions

These are deliberately unresolved; revisit when the relevant work starts.

- **Command placement** — per-domain service methods with request structs
  (current plan, matching `hivecore/hive/app.go`) versus a flat
  `app/command` + `app/query` package that gives MCP and CLI generation one
  place to enumerate. The httpapi operations table (ADR self-describing-agent-api) is an
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
