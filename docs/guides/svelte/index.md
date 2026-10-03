# Svelte guides

These guides cover `@pbvex/svelte`, a Svelte 5 rune layer over `@pbvex/client`.

## Topics

- [Client and query lifecycle](client-stores.md) — `setClient`, `getClient`, and automatic watch cleanup.
- [Public functions](functions.md) — rune-backed `useQuery`, mutations, actions, and conditional queries.
- [Testing](testing.md) — mounting a Svelte component and asserting rune state.

## Core concepts

- `setClient` stores a `Client` in Svelte context during component initialization.
- `useQuery` returns reactive `QueryState<T>` with `{ data, error, isLoading }`.
- Pass an args getter to make a query follow component state or `$props()`.
- `useMutation` and `useAction` return typed async callables.
- `skip` is the exported string literal `'skip'` for conditional queries.

## Importing generated references

SvelteKit derives TypeScript `paths` from `kit.alias`, so configure the alias there instead of relying on the `#pbvex/*` subpath import:

```ts
// vite.config.ts
export default defineConfig({
  plugins: [
    sveltekit({
      alias: { $pbvex: 'pbvex' },
    }),
  ],
});
```

Projects that still use a `svelte.config.js` put the same entry under `kit`: `kit: { alias: { $pbvex: 'pbvex' } }`. The path is relative to the SvelteKit project root, so point it at wherever your `pbvex/` folder lives.

Then import with `import { api } from '$pbvex/_generated/api'`. Do not use the bare `pbvex/_generated/api`; it resolves to the npm package.

## Cross-links

- Client semantics are documented in the [client guides](../client/index.md).
- Server-side concepts are in the core guides: [Authoring functions](../functions.md), [Schema](../schema-and-database.md), [Authentication](../auth.md).
