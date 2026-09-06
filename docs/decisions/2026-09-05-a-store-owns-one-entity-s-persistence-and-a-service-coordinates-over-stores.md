# A store owns one entity's persistence and a service coordinates over stores

- **Status:** proposed
- **Date:** 2026-09-05

## Context

One `*store.DB` holds 71 entity-prefixed methods over 14 tables. No type carries
an entity name, so every method repeats it. Services either hold the whole
object or define a narrow interface over it. `db.Queries()` leaks sqlc rows to
seven production sites.

`activity.Store` and `jobs.Store` already own one entity's persistence over the
same database. The vendored `internal/hivecore/data/stores/` does the same for
seven entities.

## Decision

A store owns one aggregate's persistence. It is built from `*queries.DB`, takes
`context.Context` first on every method, returns hand-written domain types, and
publishes no events. A service coordinates stores and other services, maps
store errors to `app.Error` kinds, and publishes events. Only services hang off
`App`.

The placement rule has four clauses:

1. A store owns one aggregate root, not one table.
2. A store method may open its own transaction and call sibling stores when the
   operation is atomic and belongs to its aggregate.
3. A service opens `Stores.Tx(ctx)` when an operation spans aggregates or
   carries policy.
4. Whole-database maintenance stays on `queries.DB`.

Every store call, including reads, goes through `.Ctx(ctx)` so it joins an
ambient transaction. `Compact` is the exception because SQLite cannot run
`VACUUM` inside a transaction. Stores map generated rows through a
`mapXFromDb` mapper, so a generated sqlc row never leaves a store. The aggregate
exists only for construction and `Tx`.

Persistence moves to `internal/app/data/{models,queries,stores}`. `models`
holds domain types without a database dependency, `queries` holds sqlc output,
the DB handle, and migrations, and `stores` holds aggregate persistence.

## Consequences

`internal/app/store` disappears as a name. A multi-table write belongs to its
owning store or a service, so no third store category grows around cross-table
work. The generated package stays behind the store boundary, so consumers hold
a store rather than a database.
