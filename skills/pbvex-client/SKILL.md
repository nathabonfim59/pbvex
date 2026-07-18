---
name: pbvex-client
description: Implement or troubleshoot typed PBVex client calls, configuration, structured errors, cancellation, and client-side storage transfers with @pbvex/client. Use for browser, Node, or framework-agnostic integration; pair with pbvex-auth or pbvex-realtime for those subsystems.
---

# PBVex typed client

Use `@pbvex/client` with generated public references; application code must install the runtime package separately from this skill.

```ts
import { Client } from '@pbvex/client';
import { api } from '../pbvex/_generated/api.js';

const client = new Client('http://127.0.0.1:8090');
const rows = await client.query(api.messages.list, { channel: 'general' });
```

Call only public `query`, `mutation`, and `action` functions. Prefer generated references for typed visibility, arguments, and returns. String paths are a supported untyped fallback for dynamic or already-typed integrations; do not manufacture them when generated references are available. Never use deployment superuser tokens in a client. Read current exports/options before changing integration code:

```bash
sed -n '1,220p' packages/client/src/index.ts
rg -n "class Client|watch\(|authStore|authWith|setAuth|generateUpload|PBVexError" packages/client/src docs/guides
pnpm --filter @pbvex/client test
```

## Auth and calls

PBVex includes PocketBase auth routes and a native client auth API. Use `pbvex-auth` for collection provisioning, auth methods, stores, SSR, and token lifecycle rather than hand-written fetches.

For externally managed tokens, provide an application record token as `auth` (string or provider) or use `setAuth`/`clearAuth`; per-call auth can override it. An app token identifies a PocketBase record and cannot deploy. Never put a PocketBase superuser deployment token in client code. Server functions remain responsible for authorization and ownership regardless of sign-in method.

Use `ClientOptions` for injected `fetch`, URL/base URL, timeout, auth store, client-side limits, or a test realtime transport. Close clients with `client.close()` when their owner is destroyed; this is distinct from canceling in-flight calls.

## Realtime

Use `client.watch` for SSE query results and always retain its unsubscribe function. Load `pbvex-realtime` for initial-result, deduplication, retry, proxy, auth refresh, and transport-test semantics.

## Errors and cancellation

Handle structured backend failures with `PBVexError` and inspect its `code`, `details`, and `requestId`. Keep transport/proxy failures distinct from structured application errors. Pass `AbortSignal` in call options for cancellation and treat its `AbortError` separately from backend rejection. Inspect `client.connectionState` and realtime callbacks when diagnosing reconnects.

## Storage

Storage is a two-step flow: an authorized server function creates a short-lived, single-use upload URL, the client POSTs bytes, then stores the returned opaque `StorageId`. Read trusted upload metadata instead of decoding image dimensions in the browser. Do not invent upload endpoints or retry a consumed URL.

Download URLs come from authorized server functions. Identity mode is the default; when created by an authenticated function it requires that same caller token, while an anonymously created identity URL needs no `Authorization` header but is itself sensitive until expiry. Capability mode is a short-lived bearer URL suitable for an image `src`; public mode is stable and CDN-cacheable for intentionally public assets. Use the `pbvex-storage` skill for image policies, thumbnail selectors, metadata trust boundaries, and lifecycle details.
