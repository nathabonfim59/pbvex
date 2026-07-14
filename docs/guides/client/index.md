# Client guides

These guides cover the browser-neutral `@pbvex/sdk-core` client. For framework bindings, see the [React guides](../react/index.md) and [Svelte guides](../svelte/index.md).

## Topics

- [Installation](installation.md) — packages and environment requirements.
- [Configuration](configuration.md) — base URL, fetch injection, timeouts, auth, and limits.
- [Queries, mutations, and actions](queries-mutations-actions.md) — typed and string-based calls, optional args, call options, cancellation.
- [Realtime subscriptions](realtime.md) — `watch` lifecycle, connection state, retry behavior, cleanup, max event handling.
- [Errors and troubleshooting](errors.md) — `PBVexError`, error codes, timeout, cancellation, and diagnostics.

## Core concepts

- Generated API references live in `pbvex/_generated/api.ts` and are imported as `FunctionReference` objects.
- `Client` is the single entry point for HTTP calls and SSE realtime subscriptions.
- `PBVexClient` is a compatibility subclass of `Client`.
- All calls are `Promise`-based and use `AbortSignal` for cancellation.

## Cross-links

- Server-side authoring is documented in the core guides: [Authoring functions](../functions.md), [Schema and data model](../schema-and-database.md), [Authentication](../auth.md), [Storage](../storage.md).
- Protocol details are in the [Protocol v1 ADR](../../adr/001-protocol-v1.md).
- Backend operation is covered in the [Operator guide](../../operator.md).
