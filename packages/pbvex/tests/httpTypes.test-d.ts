import type { ActionCtx, HttpActionCtx, MutationCtx, QueryCtx } from '../src/runtime/server.js';

declare const action: ActionCtx;
const response = await action.http.send({
  url: 'https://payments.example.test/checkouts',
  method: 'POST',
  headers: { authorization: 'Bearer secret', 'content-type': 'application/json' },
  body: JSON.stringify({ plan: 'premium' }),
  timeoutMs: 5_000,
});
void response.statusCode;
void response.headers['content-type'];
void response.body;
void response.json;

declare const httpAction: HttpActionCtx;
void httpAction.http.send({ url: 'https://example.test/' });

declare const query: QueryCtx;
// @ts-expect-error queries cannot send outbound HTTP requests
void query.http.send({ url: 'https://example.test/' });

declare const mutation: MutationCtx;
// @ts-expect-error mutations cannot send outbound HTTP requests
void mutation.http.send({ url: 'https://example.test/' });

// @ts-expect-error unsupported methods are rejected by the authoring types
void action.http.send({ url: 'https://example.test/', method: 'CONNECT' });
