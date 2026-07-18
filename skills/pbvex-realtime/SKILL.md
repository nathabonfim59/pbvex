---
name: pbvex-realtime
description: Implement, debug, review, or operate PBVex realtime query subscriptions over SSE, including client.watch, initial loading, canonical deduplication, retry ownership, auth reconnects, activation reconnects, proxy streaming, cleanup, limits, and mock transports.
---

# PBVex realtime subscriptions

Use `client.watch(queryRef, args, callbacks)` for a bounded, authorized query result. The authenticated POST SSE response supplies the initial result; the client does not make a separate initial query. `onUpdate.error` drives query state; `onError` may receive the same structured query failure as well as transport/parser failures.

PBVex conservatively reruns active subscription queries after successful record writes and emits only canonically changed results. Keep subscribed queries indexed and bounded because dependency tracking is not table- or record-specific.

Multiple watchers with the same canonical path and arguments share one subscription. The first watcher owns reconnect settings. Unsubscribe every watcher; the last unsubscribe closes that stream, and the true application owner must call `client.close()`. Reconnect exhaustion disposes the subscription.

Auth-store or explicit auth changes reconnect active streams. Deployment activation/rollback also forces reconnect so new subscriptions use the new deployment snapshot; an admitted server subscription uses its pinned snapshot until then. Treat query errors, transport errors, and aggregate connection state separately.

Reverse proxies must preserve streaming for `POST /api/pbvex/realtime`: disable response buffering, allow long-lived requests, and keep the SSE content type. Respect negotiated event-size limits and do not replace the fetch transport with browser `EventSource`, which cannot provide the same POST/auth contract.

For tests, inject a mock `RealtimeTransport` and cover initial state, canonical argument changes, retries, auth refresh, deduplication, unsubscribe, and owner cleanup. React/Svelte lifecycle details belong to their framework skills. Use `docs/guides/client/realtime.md` and `docs/guides/limits.md` as authoritative references.
