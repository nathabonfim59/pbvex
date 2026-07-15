# Authentication

PBVex distinguishes application-record tokens from deployment credentials. Application tokens identify a PocketBase auth record for public calls, realtime, storage downloads, and HTTP actions. PocketBase superuser tokens authorize deployment upload/activation/rollback and scheduler administration; they are never application identities.

## PocketBase authentication is included

PBVex embeds PocketBase and leaves its standard `/api/collections/{collection}/...` authentication routes intact. `@pbvex/client` implements those routes directly, including a PocketBase-compatible auth store and OAuth2 popup flow, so a PBVex application does not need the additional `pocketbase` JavaScript package just for authentication. PBVex does not maintain a second user store or a separate OAuth callback.

The bundled PocketBase authentication features are therefore available to PBVex applications:

- identity/password authentication;
- email one-time passwords (OTP);
- OAuth2, including the all-in-one popup flow and manual authorization-code exchange;
- multi-factor authentication (MFA) using two different enabled methods;
- auth refresh/token verification; and
- superuser-authorized user impersonation.

See PocketBase's [authentication overview](https://pocketbase.io/docs/authentication/) for the security model and complete flows. Each method must be enabled on the particular auth collection before it can be used.

### Configure an auth collection

Open the Admin UI, select or create an auth collection such as `users`, then open the collection settings and its authentication options. Enable password, OTP, MFA, or the OAuth2 providers required by the application. OTP requires working mail settings. OAuth2 requires credentials from each provider and a registered callback URL of:

```text
https://your-domain.example/api/oauth2-redirect
```

PBVex supports every OAuth2 provider exposed by its bundled PocketBase version, rather than keeping a separate provider allowlist. This includes Apple, Google, Microsoft, Yandex, Facebook, Instagram, GitHub, GitLab, Bitbucket, Gitee, Gitea/Forgejo, Discord, X/Twitter, Kakao, VK, Linear, Notion, monday.com, Lark, Box, Spotify, Trakt, Twitch, Patreon, Strava, WakaTime, LiveChat, mailcow, Planning Center, and configurable OIDC entries. Consult PocketBase's [OAuth2 guide](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2) for provider setup, redirect behavior, and the manual exchange flow.

OAuth2 is supported for application auth collections, but PocketBase does **not** support OAuth2 for the `_superusers` collection. Use a superuser password or another PocketBase-supported superuser method for deployment credentials.

## Sign in with the PBVex client

Install the PBVex client:

```bash
npm install @pbvex/client
```

```ts
import { PBVexClient } from '@pbvex/client';

const url = 'https://api.example.test';
const client = new PBVexClient(url);
const users = client.auth.collection('users');

await users.authWithPassword(
  'person@example.com',
  'correct horse battery staple',
);

console.log(client.authStore.isValid);
console.log(client.authStore.record?.id);
```

Successful password, OTP, OAuth2, and refresh calls save the returned token and record in `client.authStore`. PBVex function calls and realtime subscriptions read that store automatically, and already-live subscriptions reconnect when it changes. In a browser the default `LocalAuthStore` persists under `pbvex_auth`; outside a browser it falls back to memory. Pass `authStore: new AuthStore()` for explicitly memory-only state, or inject a custom `LocalAuthStore` storage implementation for another runtime.

The token's sign-in method is not encoded into PBVex authorization behavior. A valid record token obtained through password, OTP, OAuth2, MFA completion, auth refresh, or user impersonation identifies the same auth record in `ctx.auth`.

### OTP

Enable OTP on the auth collection and configure mail delivery first. Then request and redeem the emailed code:

```ts
const request = await users.requestOTP('person@example.com');
await users.authWithOTP(
  request.otpId,
  codeEnteredByUser,
);
```

PocketBase deliberately returns an `otpId` even when the address does not exist. Do not use the response to infer account existence. PocketBase also recommends combining OTP with MFA for security-sensitive applications; see [Authenticate with OTP](https://pocketbase.io/docs/authentication/#authenticate-with-otp).

### OAuth2

After enabling and configuring a provider on the auth collection, start PocketBase's all-in-one flow from a direct user interaction so popup blockers do not intercept it:

```ts
users.authWithOAuth2({ provider: 'google' }).then((authData) => {
  console.log(authData.record.id);
});
```

The client opens the provider popup, uses a one-off PocketBase realtime subscription to receive the result, and stores the returned record token. Call it directly from a user interaction so Safari and other popup blockers allow the window. A custom `urlCallback` can open the generated URL in a platform-specific browser.

For mobile applications, custom redirects, or page-based flows, use `listAuthMethods()` to obtain the provider data and finish with `authWithOAuth2Code(provider, code, codeVerifier, redirectURL, createData?)`. Follow PocketBase's [manual OAuth2 code exchange](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2) for the state check and Apple-specific callback behavior.

### MFA

PocketBase MFA requires two different enabled authentication methods. The first successful method responds with HTTP 401 and an `mfaId`; perform a second method and include that ID. For example, password followed by OTP:

```ts
try {
  await users.authWithPassword('person@example.com', password);
} catch (error: any) {
  const mfaId = error.response?.mfaId;
  if (!mfaId) throw error;

  const request = await users.requestOTP('person@example.com');
  await users.authWithOTP(request.otpId, otpCode, { mfaId });
}
```

The second method returns the normal record token. See PocketBase's [MFA flow](https://pocketbase.io/docs/authentication/#multi-factor-authentication) for configuration and method-order details.

### Verification, password reset, and email change

The same collection auth service covers the email-driven account lifecycle:

```ts
await users.requestVerification('person@example.com');
await users.confirmVerification(tokenFromVerificationLink);

await users.requestPasswordReset('person@example.com');
await users.confirmPasswordReset(resetToken, newPassword, newPassword);

await users.requestEmailChange('new@example.com');
await users.confirmEmailChange(emailChangeToken, currentPassword);
```

The request methods intentionally return success without revealing whether an email exists, matching PocketBase's enumeration protections. Mail delivery and the built-in message templates are configured on the auth collection in the Admin UI.

## Client token lifecycle

`client.authStore` exposes `token`, `record`, `isValid`, `isSuperuser`, `save()`, `clear()`, and `onChange()`. Refresh the current record and token through the same collection service:

```ts
await users.authRefresh();
```

An auth refresh does not invalidate the previous token. PocketBase impersonation tokens are not renewable. A superuser can request impersonated auth data without replacing its own auth store:

```ts
const impersonated = await users.impersonate(userId, 3600);
const impersonatedClient = new PBVexClient(url, { auth: impersonated.token });
```

PocketBase auth is stateless and has no logout endpoint. Discard the local token to log out; this also reconnects live PBVex subscriptions without auth:

```ts
client.authStore.clear();
```

`setAuth()` and `clearAuth()` remain available for externally managed or service tokens, and a per-call `auth` option can override the default source. Clearing an externally configured source does not revoke a PocketBase token.

See PocketBase's [auth token verification and refresh notes](https://pocketbase.io/docs/authentication/#auth-token-verification).

## Identity in a function

Every function context has `ctx.auth.getUserIdentity()`. It resolves to `null` for an unauthenticated request; do not assume a public function has a user.

```ts
import { mutation } from './_generated/server';
import { v } from 'pbvex/values';

export const createPrivateMessage = mutation({
  args: { body: v.string() },
  handler: async (ctx, { body }) => {
    const user = await ctx.auth.getUserIdentity();
    if (!user) throw new Error('unauthenticated');
    return ctx.db.insert('messages', { body, owner: user.tokenIdentifier });
  },
});
```

An identity includes stable `subject`, `issuer`, and `tokenIdentifier`, plus selected optional profile claims such as `email` and `name`. Store `tokenIdentifier` when you need a durable ownership key. It is a portable representation, not the underlying PocketBase record and not a claim that the user may access a particular document.

## Authorize access after authentication

Authentication does not grant access to application data. Every sensitive public function must enforce ownership, membership, tenant, or role rules using the resolved identity. See the dedicated [Authorization](./authorization.md) guide for a complete instant-messaging example covering protected reads, writes, subscriptions, and direct API bypass prevention.

Nested calls preserve the originating identity and request metadata. A scheduled job is a later background invocation, not a live user request: do not use it as proof of an authenticated session; pass and re-check the durable authorization data your job needs. HTTP actions receive the same optional app identity, while webhook signatures are a separate authentication mechanism.

Never place a superuser deployment token in a web client, bundle, or `pbvex.config.ts`. Keep it in deployment environment/credentials as described in [the self-hosting guide](../self-hosting.md#bootstrap-deployment-credentials). A valid application token cannot deploy code, and a superuser token is not exposed through `ctx.auth` as an app user.
