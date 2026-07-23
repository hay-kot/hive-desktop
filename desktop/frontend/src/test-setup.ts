import { beforeEach } from 'vitest'

// Node (22+) ships its own global `localStorage`/`sessionStorage`, which
// shadows happy-dom's Storage implementation in this Vitest environment and
// throws ("getItem is not a function") without a `--localstorage-file`
// backing path. Every real runtime target (a browser, the Wails webview) has a
// working localStorage, so this in-memory stand-in exists purely so tests
// exercise the same code paths (useTheme, useResizablePanel, ...) as
// production.
//
// It is installed ONCE, at setup-module load — before any test file (and its
// module-singleton `useStorage` calls, e.g. useTheme) evaluates — so those
// capture a working, stable Storage reference. beforeEach then `clear()`s that
// same instance (rather than swapping in a new one) to keep specs isolated
// without invalidating references already captured by singletons.
function memoryStorage(): Storage {
  const values = new Map<string, string>()
  return {
    get length() { return values.size },
    clear: () => values.clear(),
    getItem: (key: string) => values.get(key) ?? null,
    key: (index: number) => [...values.keys()][index] ?? null,
    removeItem: (key: string) => values.delete(key),
    setItem: (key: string, value: string) => values.set(key, value),
  }
}

Object.defineProperty(globalThis, 'localStorage', {
  value: memoryStorage(),
  writable: true,
  configurable: true,
})

// Unit tests must never open real sockets. happy-dom's default origin is
// http://localhost:3000, so an unmocked Wails binding call (@wailsio/runtime
// POSTs to <origin>/wails/runtime via fetch) otherwise dials a real port —
// and a refused connection can surface as an uncaught AggregateError outside
// any promise chain, failing a CI run whose tests all passed. A synthetic 503
// keeps the callers on the same rejected-promise path they already handle,
// with no socket involved. Specs that need a different fetch can still stub
// their own (the property stays writable).
Object.defineProperty(globalThis, 'fetch', {
  value: () => Promise.resolve(new Response('{"error":"network disabled in unit tests"}', {
    status: 503,
    headers: { 'Content-Type': 'application/json' },
  })),
  writable: true,
  configurable: true,
})

beforeEach(() => {
  localStorage.clear()
})
