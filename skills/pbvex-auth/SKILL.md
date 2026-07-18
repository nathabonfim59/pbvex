---
name: pbvex-auth
description: Implement or troubleshoot PBVex and PocketBase application authentication, including auth collections, signup, password, OTP, OAuth2, MFA, auth stores, refresh, impersonation, logout, SSR initialization, and server-side authorization. Use for user identity and token lifecycle work, not deployment credentials.
---

# PBVex application authentication

Keep application-record tokens separate from PocketBase superuser deployment credentials. App tokens identify an auth-collection record for calls, realtime, storage, and HTTP actions; they cannot deploy. Never ship a superuser token to a browser.

## Provision and authenticate

Configure each auth method on the intended PocketBase auth collection. Keep collection fields and rules reproducible with typed `pbvex/pocketbaseMigrations/` host migrations rather than dashboard-only state. OAuth2 uses PocketBase's `/api/oauth2-redirect`; `_superusers` do not support OAuth2.

Use `client.auth.collection('users')` instead of hand-written auth fetches. Record creation does not sign in. Password, OTP, OAuth2, MFA completion, and `authRefresh()` save successful record auth into `client.authStore`; `AuthApiError` retains PocketBase field errors. MFA's first method returns HTTP 401 with `response.mfaId`; complete it with a different enabled method carrying that ID. Start OAuth2 popup auth from a direct user interaction; manual/mobile exchanges must preserve and verify state. Enumeration-safe email/OTP requests may return success for unknown addresses.

The browser default `LocalAuthStore` persists under `pbvex_auth` and notifies live clients; outside a browser it falls back to memory. Inject `new AuthStore()` for memory-only state or a custom store for another runtime. Browser clients may be application-scoped, but SSR/server clients and auth stores must be request-scoped—never share user tokens across requests. Initialize browser-backed storage only on the client. Auth-store changes reconnect active realtime subscriptions.

PocketBase tokens are stateless: logout is `client.authStore.clear()`, not a server endpoint. Use `setAuth`/`clearAuth` only for externally managed token sources; per-call auth can override either source. Refresh does not revoke an old token, and impersonation tokens are not renewable.

## Authorize on the server

`ctx.auth.getUserIdentity()` returns `null` or portable record identity. Store `identity.tokenIdentifier` as the durable application ownership key; do not key ownership by editable email or optional profile claims. Authentication method does not change authorization semantics. Every sensitive public function must verify ownership, membership, tenant, or role from durable data; hiding UI or possessing a valid ID/token is insufficient.

Use `docs/guides/auth.md` and PocketBase's version-matched authentication docs for complete method setup. Test auth-store persistence/clear/change behavior, failure data, token propagation, and realtime reconnection without logging tokens or secrets.
