---
name: pbvex-react
description: Build, review, or test React integrations using @pbvex/react, including PBVexProvider, typed query/mutation/action hooks, realtime lifecycle, error handling, and mock-client tests. Use for React components that consume PBVex.
---

# PBVex React

Install `@pbvex/react` and its `@pbvex/client` peer separately. Create one client for the application owner and provide it near the root:

```tsx
import { Client } from '@pbvex/client';
import { PBVexProvider } from '@pbvex/react';

const client = new Client('http://localhost:8090');
export function Root({ children }: { children: React.ReactNode }) {
  return <PBVexProvider client={client}>{children}</PBVexProvider>;
}
```

Use generated public `api` references with `useQuery`, `useQueryResult`, `useMutation`, and `useAction`. `useQuery` returns `undefined` while loading and throws query errors to an error boundary; use `useQueryResult` for explicit `{ data, error, isLoading }` rendering. Pass `'skip'` to disable a conditional query. Canonically equal inline arguments do not resubscribe, so do not add `useMemo` solely for PBVex. `useSubscription` is a compatibility alias for `useQuery`.

`useMutation` and `useAction` return stable typed async callables. `usePBVexClient` exposes the provider client and throws outside the provider. The provider does not close a client: the owning application/test must close it.

Server rendering receives the initial loading snapshot and does not open a subscription; hydrate live data on the client. PBVex has no built-in optimistic cache mutation layer, so model explicit local optimistic state only when needed. Use `pbvex-auth` for browser auth-store initialization and `pbvex-realtime` for transport behavior.

## Test lifecycle, not implementation details

Use Vitest, `renderHook`, a `PBVexProvider` wrapper, and a mock `RealtimeTransport` injected into `Client`. Assert loading/data/error, argument changes, stable mutation callable identity, and cleanup (including React StrictMode double subscriptions). Prefer generated references in app tests; synthetic typed refs are appropriate only for isolated SDK-hook tests.

```bash
pnpm --filter @pbvex/react test
sed -n '1,240p' docs/guides/react/testing.md
rg -n "useQuery|PBVexProvider" packages/react/src
```

Do not call hooks conditionally or let a short-lived component silently own a shared client. Authorization remains server-side even when UI state hides controls.
