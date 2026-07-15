# Outbound HTTP requests

Actions and HTTP actions can call external APIs through `ctx.http.send`. Use it for payment providers, transactional APIs, webhooks you deliver, and bounded reads from external services.

```ts
import { action } from './_generated/server';
import { v } from 'pbvex/values';

export const createCheckout = action({
  args: { customerId: v.string() },
  handler: async (ctx, { customerId }) => {
    const response = await ctx.http.send({
      url: 'https://payments.example.com/v1/checkouts',
      method: 'POST',
      headers: {
        authorization: 'Bearer replace-with-component-secret',
        'content-type': 'application/json',
      },
      body: JSON.stringify({ customerId }),
      timeoutMs: 10_000,
    });

    if (response.statusCode !== 201) {
      throw new Error(`provider returned ${response.statusCode}`);
    }
    return response.json;
  },
});
```

The capability corresponds to the underlying server runtime's external-request support, while keeping the PBVex function boundary typed and bounded. See the underlying [$http.send documentation](https://pocketbase.io/docs/js-sending-http-requests/) for the related PocketBase concept.

## Request options

| Option | Type | Behavior |
| --- | --- | --- |
| `url` | `string` | Required absolute `http` or `https` URL without embedded user information |
| `method` | `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, or `DELETE` | Defaults to `GET` |
| `headers` | `Record<string, string>` | Optional request headers; runtime-controlled transport headers are rejected |
| `body` | `string`, `Uint8Array`, or `ArrayBuffer` | Optional body, limited to 1 MiB |
| `timeoutMs` | `number` | Defaults to 10 seconds; must be between 1 ms and 30 seconds |

The call also remains bounded by the enclosing function deadline. A timeout or network failure rejects the promise.

## Response

`send` resolves after reading the complete response:

```ts
interface HttpSendResponse {
  statusCode: number;
  headers: Readonly<Record<string, readonly string[]>>;
  body: Uint8Array;
  json: unknown | null;
}
```

`json` contains the parsed JSON value when the complete body is valid JSON; otherwise it is `null`. Inspect `statusCode` before trusting either representation. Response bodies are limited to 4 MiB and are not streamed.

Redirects are returned as ordinary 3xx responses and are not followed automatically. This prevents an authorization header from being silently forwarded to another origin. If a provider documents a redirect, validate its `Location` header against an allowlist and issue a second request intentionally.

## Capability boundary

Only actions and HTTP actions receive `ctx.http.send`. Queries must remain deterministic reads, while mutations keep database writes transactional and cannot wait on external side effects. Use an action to call the provider and internal mutations to record state before or after the call.

Components can combine outbound HTTP with explicit [environment bindings](./environment-variables.md). This is the recommended boundary for provider credentials:

```ts
const response = await ctx.http.send({
  url: `${ctx.env.API_BASE_URL}/v1/checkouts`,
  method: 'POST',
  headers: { authorization: `Bearer ${ctx.env.API_KEY}` },
  body: JSON.stringify(payload),
});
```

## Security and reliability

- Never accept an arbitrary destination URL from a client. Allowlist the provider host in server code to prevent SSRF and internal-network probing.
- Keep API keys in component environment bindings and never return or log them.
- Set a timeout below the enclosing function deadline and handle non-2xx responses explicitly.
- Treat provider responses as untrusted input. Check the JSON shape before passing it to a mutation.
- Design external writes and follow-up mutations to be idempotent. A function can fail after the provider accepted the request but before local state was updated.
- Do not use outbound HTTP as a substitute for indexed application data. Persist the provider identifiers and state needed by normal queries.

See [Project organization](./project-organization.md), [Functions](./functions.md), [HTTP actions](./http-actions.md), and the [FakePayment tutorial](../tutorials/payments/) for a complete checkout and webhook flow.
