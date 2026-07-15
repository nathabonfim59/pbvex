# React guides

These guides cover `@pbvex/react`, a React hook layer over `@pbvex/client`.

## Topics

- [Provider](provider.md) — `PBVexProvider` and `PBVexContext`.
- [Hooks](hooks.md) — `useQuery`, `useQueryResult`, `useMutation`, `useAction`, `useSubscription`, and `usePBVexClient`.
- [Patterns](patterns.md) — loading, error boundaries, `skip`, stable args, and cleanup.
- [Testing](testing.md) — `renderHook`, `PBVexProvider`, and mock transports.

## Core concepts

- One `PBVexProvider` provides a `Client` to the component tree.
- `useQuery` returns the latest query value or `undefined` while loading.
- `useQueryResult` returns the full `{ data, error, isLoading }` state.
- Mutations and actions are stable callables returned by hooks.
- All subscriptions use `useSyncExternalStore` with a shared `QueryStore`.

## Cross-links

- Underlying client behavior is documented in the [client guides](../client/index.md).
- Server-side concepts are in the core guides: [Authoring functions](../functions.md), [Schema](../schema-and-database.md), [Authentication](../auth.md).
