---
name: pbvex-svelte
description: Build, review, or test Svelte 5 PBVex integrations using @pbvex/svelte, with runes-first query state, context-provided clients, reactive arguments, conditional queries, and component-lifecycle cleanup. Use for Svelte components consuming PBVex.
---

# PBVex Svelte 5

Use `@pbvex/svelte` as a Svelte 5, runes-first layer over an independently installed `@pbvex/client`. Set a client in Svelte context during component initialization, then use the current package exports from child components. Prefer rune state and markup reads over Svelte stores.

The Svelte package is actively migrating. Inspect the installed/versioned exports and types before coding or documenting an exact signature; do not hardcode unstable signatures from memory.

```bash
sed -n '1,240p' packages/svelte/src/index.ts
sed -n '1,240p' packages/svelte/package.json
rg -n "export |useQuery|setClient|skip|QueryState" packages/svelte/src packages/svelte/dist
pnpm --filter @pbvex/svelte test
```

## Runes-first patterns

Use `setClient` in a layout/root during initialization and `getClient` only where context is available. Use the primary query utility with generated public references. Treat query output as reactive rune-backed state and read its `data`, `error`, and loading fields directly in markup—do not `$`-subscribe or use `svelte/store` helpers unless the installed API explicitly says it exposes a store.

When arguments depend on `$state()` or `$props()`, supply the supported reactive getter so a changed argument replaces the watch. Use the exported `skip` sentinel for a disabled conditional query. Use the mutation/action utilities for typed async calls. Let component destruction clean up query subscriptions, and explicitly close a client only at its true application/test owner.

## Testing

Mount a small Svelte harness so rune APIs run during component initialization. Inject a mock client where the installed API supports it, drive state with Svelte test utilities, then assert reactive state and unsubscribe cleanup after unmount. Do not test current rune query state as a legacy readable store.

Do not edit `packages/svelte` or Svelte documentation as part of application integration work unless explicitly asked; this skill is intentionally resilient to in-progress package changes.
