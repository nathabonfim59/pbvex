# Svelte guides

These guides cover `@pbvex/sdk-svelte`, a Svelte store layer over `@pbvex/sdk-core`.

## Topics

- [Client and context](client-stores.md) — `setClient`, `getClient`, and store lifecycle.
- [Public functions](functions.md) — `useQuery`, `useQueryResult`, `useSubscription`, `createQuery`, `useMutation`, and `useAction`.
- [Testing](testing.md) — mocking the client and testing Svelte stores.

## Core concepts

- `setClient` stores a `Client` in Svelte context during component initialization.
- `useQuery` returns a `Readable` of the value (`undefined` while loading or on error).
- `useQueryResult` returns a `Readable<QueryResult<T>>` with `{ data, error, isLoading }`.
- `useMutation` and `useAction` return typed async callables.
- `skip` is the exported string literal `'skip'` for conditional queries.

## Cross-links

- Client semantics are documented in the [client guides](../client/index.md).
- Server-side concepts are in the core guides: [Authoring functions](../functions.md), [Schema](../schema-and-database.md), [Authentication](../auth.md).
