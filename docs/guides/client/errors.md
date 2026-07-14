# Errors and troubleshooting

`@pbvex/sdk-core` distinguishes between structured PBVex errors, transport errors, and cancellation errors.

## PBVexError

Structured errors returned by the backend are thrown as `PBVexError`:

```ts
import { Client, PBVexError } from '@pbvex/sdk-core';
import { api } from '../pbvex/_generated/api.js';

const client = new Client('http://localhost:8090');

try {
  await client.query(api.messages.get, { messageId: 'unknown' });
} catch (error) {
  if (error instanceof PBVexError) {
    console.log(error.code);      // e.g. 'not_found'
    console.log(error.message);   // human-readable message
    console.log(error.details);   // optional detail array
    console.log(error.requestId); // optional request id
  }
}
```

`PBVexError` properties:

| Property | Type | Description |
| --- | --- | --- |
| `error` | `true` | Constant discriminator. |
| `code` | `ErrorCode` | Structured error code. |
| `message` | `string` | Human-readable message. |
| `details` | `unknown[] \| undefined` | Optional details. |
| `requestId` | `string \| undefined` | Optional request id. |
| `structuredError` | `StructuredError` | Raw structured error object. |

## Error codes

Core codes are:

`bad_request`, `invalid_manifest`, `invalid_function`, `bundle_not_found`, `bundle_hash_mismatch`, `activation_failed`, `not_found`, `unauthorized`, `forbidden`, `internal`.

Service-specific codes include storage admission states:

`upload_expired`, `upload_consumed`, `upload_pending`, `upload_too_large`, `invalid_content`, `storage_full`.

The SDK accepts the error codes exported by `@pbvex/protocol`. A response with an unknown code is treated as an unstructured HTTP error rather than a `PBVexError`.

## Call errors

| Symptom | Likely cause |
| --- | --- |
| `HTTP 4xx/5xx` with non-JSON body | Backend or reverse proxy returned an unstructured response. |
| `Malformed response: missing or invalid result` | Response JSON does not have a top-level `result` field. |
| `Unexpected response content-type` | Response is not `application/json`. |
| `Function args exceed N bytes` | Encoded args exceed `maxFunctionArgsBytes`. |
| `Request body exceeds N bytes` | Total call body exceeds the safety limit. |
| `Request timeout after Nms` | `timeoutMs` fired before a response or body read completed. |

## Realtime errors

| Symptom | Likely cause |
| --- | --- |
| `Realtime connection failed: HTTP N` | Initial SSE POST returned a non-2xx response. |
| `Realtime connection failed: unexpected content-type` | Response is not `text/event-stream`. |
| `Malformed SSE event data` | A `data:` line is not valid JSON. |
| `Invalid SSE envelope` | A parsed event does not match the `RealtimeEnvelope` shape. |
| `Realtime reconnect limit reached` | `maxReconnects` was exhausted. |
| `Invalid UTF-8 in SSE stream` | The stream was truncated on a multibyte boundary. |

## Cancellation

`AbortSignal` cancellation throws `AbortError`:

```ts
const controller = new AbortController();
const promise = client.query(api.messages.list, { channel: 'general' }, {
  signal: controller.signal,
});
controller.abort();

try {
  await promise;
} catch (error) {
  if (error instanceof Error && error.name === 'AbortError') {
    // cancelled
  }
}
```

## Debugging

- Enable `dev` logging on the backend (`pbvex serve --dev`) to see request details.
- Inspect `client.connectionState` during realtime reconnect loops.
- Use `response.headers.get('content-type')` to diagnose reverse proxy responses.
- Keep `PBVEX_TOKEN` out of client code; use client-side auth providers or PocketBase record tokens.
