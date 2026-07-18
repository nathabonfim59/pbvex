---
name: pbvex-client
description: Implement or troubleshoot typed PBVex client calls, built-in PocketBase application authentication, auth-store lifecycle, structured errors and cancellation, SSE realtime subscriptions, and two-step file storage with @pbvex/client. Use for browser, Node, or framework-agnostic client integration.
---

# PBVex typed client

Use `@pbvex/client` with generated public references; application code must install the runtime package separately from this skill.

```ts
import { Client } from '@pbvex/client';
import { api } from '../pbvex/_generated/api.js';

const client = new Client('http://127.0.0.1:8090');
const rows = await client.query(api.messages.list, { channel: 'general' });
```

Call only public `query`, `mutation`, and `action` references. Do not recreate paths as strings or use deployment superuser tokens in a client. Read current exports/options before changing integration code:

```bash
sed -n '1,220p' packages/client/src/index.ts
rg -n "class Client|watch\(|authStore|authWith|setAuth|generateUpload|PBVexError" packages/client/src docs/guides
pnpm --filter @pbvex/client test
```

## Auth and calls

PBVex leaves PocketBase auth routes available, and `@pbvex/client` implements them without requiring the separate `pocketbase` package. Use `client.auth.collection('users').create(...)` for signup, then authenticate explicitly; record creation does not populate the auth store. Use the same collection client for password, OTP, OAuth2, MFA completion, refresh, verification, password-reset, and email-change flows. The auth collection and required custom fields must already be configured with the intended create policy; keep that configuration reproducible in `pbvex/pocketbaseMigrations/`, not only in dashboard state. Successful authentication updates `client.authStore`; calls use it automatically and live subscriptions reconnect. Clear the store to log out because PocketBase tokens are stateless. Do not hand-roll raw PocketBase auth fetches when the collection client supports the operation.

For externally managed tokens, provide an application record token as `auth` (string or provider) or use `setAuth`/`clearAuth`; per-call auth can override it. An app token identifies a PocketBase record and cannot deploy. Never put a PocketBase superuser deployment token in client code. Server functions remain responsible for authorization and ownership regardless of sign-in method.

Use `ClientOptions` for injected `fetch`, URL/base URL, timeout, client-side limits, or a test realtime transport. Close clients with `client.close()` when their owner is destroyed; this is distinct from canceling in-flight calls.

## Realtime

Use `client.watch(queryRef, args, callbacks)` for SSE query results. `onUpdate` receives `{ data, error, isLoading }`; retain and invoke the unsubscribe function. Multiple watchers with equal canonical path/args share a subscription. Treat connection-state/reconnect callbacks as transient transport state, and expect activation/rollback or auth changes to reconnect subscriptions.

For tests, inject `fetch` or a `RealtimeTransport`; do not require a live backend for every client/UI test.

## Errors and cancellation

Handle structured backend failures with `PBVexError` and inspect its `code`, `details`, and `requestId`. Keep transport/proxy failures distinct from structured application errors. Pass `AbortSignal` in call options for cancellation and treat its `AbortError` separately from backend rejection. Inspect `client.connectionState` and realtime callbacks when diagnosing reconnects.

## Storage

Storage is a two-step, capability flow: a trusted mutation/action calls `ctx.storage.generateUploadUrl()`, the client POSTs bytes to that short-lived single-use URL, then stores the returned opaque `StorageId` in an authorized document. Do not invent upload endpoints, retry a consumed URL, or treat signed download URLs as permanent/public. Request download URLs from server code only after it checks access.
