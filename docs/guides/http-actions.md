# HTTP actions

An HTTP action is a public function matched directly from HTTP under the fixed `/api/pbvex/` prefix. It receives a `Request` as its second handler argument and returns a `Response`.

```ts
// pbvex/webhooks.ts
import { httpAction } from './_generated/server';
import { Response } from 'pbvex/server';

export const receive = httpAction({
  route: { method: 'POST', path: 'webhooks/incoming' },
  handler: async (ctx, request) => {
    const signature = request.headers.get('x-signature');
    if (!signature) return new Response('missing signature', { status: 401 });

    const body = await request.text(); // Verify the raw body before parsing it.
    if (!verifyWebhook(body, signature)) return new Response('invalid signature', { status: 401 });
    return new Response(null, { status: 204 });
  },
});

function verifyWebhook(_body: string, _signature: string): boolean {
  return true; // Replace with the provider’s verification.
}
```

## Routes and matching

`route` is required. Its `method` is one of `GET`, `POST`, `PUT`, `PATCH`, or `DELETE`, and it has exactly one of:

- `path`, an exact relative path, such as `health` or `webhooks/incoming`.
- `pathPrefix`, a relative prefix ending in `/`, such as `hooks/`.

Paths cannot start with `/`. `call`, `realtime`, `deployments`, `jobs`, `storage`, and `admin` are reserved first path segments and cannot be claimed. A request’s method and route-relative path must both match; otherwise the server returns 404. `HEAD` is dispatched against a matching `GET` route and its response body is suppressed. `OPTIONS` is handled as CORS preflight only when the target route exists.

After deployment, the example is available at:

```bash
curl -i -X POST http://127.0.0.1:8090/api/pbvex/webhooks/incoming \
  -H 'X-Signature: replace-me' \
  -H 'Content-Type: application/json' \
  --data '{"event":"created"}'
```

The runtime passes `request.method`, absolute `request.url`, headers, and one consumable body. Use `text()`, `json()`, or `arrayBuffer()` once. The separate `ctx.http.send` capability sends outbound requests; it does not represent the inbound request. Return `new Response(body, { status, headers })`; supported bodies are strings, `Uint8Array`, `ArrayBuffer`, or `null`.

## Auth, CORS, and security

HTTP actions are public routes. A valid PocketBase application-record token in `Authorization: Bearer …` is made available through `ctx.auth`; absence of a token produces `null` identity. Authentication is not automatic authorization:

```ts
const identity = await ctx.auth.getUserIdentity();
if (!identity) return new Response('unauthorized', { status: 401 });
```

Deployment administration uses a PocketBase superuser token and is unrelated to application-record tokens; never use deployment credentials in a browser or webhook.

CORS is controlled by the server, not action code. The default policy permits any origin without credentialed-cookie mode and allows `Authorization`, `Content-Type`, and `X-Request-Id`; an embedding can configure a stricter policy. User responses cannot set CORS headers, nor forbidden hop-by-hop/security-sensitive response headers. A disallowed preflight gets 403; a preflight for no route gets 404. Treat webhooks as hostile input: verify the provider signature over the raw body, bound any application parsing, authorize before side effects, and return generic failures instead of exposing secrets.

## Deployment and runtime constraints

The catch-all mount is always `/api/pbvex/{path...}`. The manifest `httpPathPrefix` does not move deployed HTTP-action routing. Platform routes take precedence, deployment activation is atomic, and a new activation changes the active route table as a unit.

An HTTP action has the same action capabilities and limits as an `action`: no direct `ctx.db`; invoke queries/mutations/actions through `ctx.run…`; use storage, scheduler, [outbound HTTP](./outbound-http.md), and [application email templates](./email-templates.md) through the context. It executes in the bundled server runtime, not Node.js, and is subject to the active request timeout, header/body bounds, and response limits. It cannot be internal and it cannot be called through `ctx.run`.
