import { test } from 'node:test';
import assert from 'node:assert';
import { parseContentType, readBoundedText, isRealtimeEnvelope, MAX_RESPONSE_BODY_BYTES } from './http.js';

function responseOfBody(body: string, headers?: Record<string, string>): Response {
  return new Response(new TextEncoder().encode(body), { status: 200, headers });
}

test('parseContentType: basic media type', () => {
  const parsed = parseContentType('application/json');
  assert.ok(parsed);
  assert.strictEqual(parsed!.type, 'application');
  assert.strictEqual(parsed!.subtype, 'json');
  assert.strictEqual(parsed!.mediaType, 'application/json');
  assert.strictEqual(parsed!.parameters.size, 0);
});

test('parseContentType: media type with charset parameter', () => {
  const parsed = parseContentType('application/json; charset=utf-8');
  assert.ok(parsed);
  assert.strictEqual(parsed!.mediaType, 'application/json');
  assert.strictEqual(parsed!.parameters.get('charset'), 'utf-8');
});

test('parseContentType: quoted parameter value', () => {
  const parsed = parseContentType('text/plain; boundary="abc=="');
  assert.ok(parsed);
  assert.strictEqual(parsed!.mediaType, 'text/plain');
  assert.strictEqual(parsed!.parameters.get('boundary'), 'abc==');
});

test('parseContentType: ignores OWS around semicolons, not around "/" or "="', () => {
  const parsed = parseContentType('text/event-stream ; charset="utf-8"');
  assert.ok(parsed);
  assert.strictEqual(parsed!.mediaType, 'text/event-stream');
  assert.strictEqual(parsed!.parameters.get('charset'), 'utf-8');

  assert.strictEqual(parseContentType('application / json'), undefined);
  assert.strictEqual(parseContentType('text/event-stream; charset = utf-8'), undefined);
});

test('parseContentType: returns undefined for missing type or subtype', () => {
  assert.strictEqual(parseContentType('/json'), undefined);
  assert.strictEqual(parseContentType('application/'), undefined);
  assert.strictEqual(parseContentType('application json'), undefined);
});

test('parseContentType: returns undefined for invalid token characters', () => {
  assert.strictEqual(parseContentType('app/json;foo=bar baz'), undefined);
  assert.strictEqual(parseContentType('app/json;foo'), undefined);
  assert.strictEqual(parseContentType('app/json;foo="'), undefined);
});

test('parseContentType: rejects NUL, DEL, and CTLs in quoted strings', () => {
  assert.strictEqual(parseContentType('text/plain; boundary="ab\x00c"'), undefined);
  assert.strictEqual(parseContentType('text/plain; boundary="ab\x7fc"'), undefined);
  assert.strictEqual(parseContentType('text/plain; boundary="ab\x1fc"'), undefined);
});

test('parseContentType: rejects duplicate parameters', () => {
  assert.strictEqual(parseContentType('text/plain; charset=utf-8; charset=utf-8'), undefined);
});

test('parseContentType: accepts valid quoted-pair escapes inside quoted strings', () => {
  const parsed = parseContentType('text/plain; boundary="a\\"b\\\\c"');
  assert.ok(parsed);
  assert.strictEqual(parsed!.parameters.get('boundary'), 'a"b\\c');
});

test('parseContentType: rejects OWS around "=" for quoted-string values', () => {
  assert.strictEqual(parseContentType('text/plain; boundary = "x"'), undefined);
});

test('parseContentType: rejects trailing garbage after media type', () => {
  assert.strictEqual(parseContentType('application/json garbage'), undefined);
  assert.strictEqual(parseContentType('application/json; charset=utf-8 extra'), undefined);
});

test('parseContentType: normalizes parameter names and values case-insensitively for names', () => {
  const parsed = parseContentType('application/json; CHARSET=utf-8');
  assert.ok(parsed);
  assert.strictEqual(parsed!.parameters.get('charset'), 'utf-8');
  assert.strictEqual(parsed!.parameters.get('CHARSET'), undefined);
});

test('isRealtimeEnvelope: accepts a subscribe control with maxEventSize negotiation', () => {
  assert.ok(isRealtimeEnvelope({ id: 'sub1', op: 'subscribe', maxEventSize: 1048576 }));
  assert.ok(isRealtimeEnvelope({ id: 'sub1', op: 'subscribe' }));
});

test('isRealtimeEnvelope: rejects invalid maxEventSize', () => {
  assert.strictEqual(isRealtimeEnvelope({ id: 'sub1', op: 'subscribe', maxEventSize: 0 }), false);
  assert.strictEqual(isRealtimeEnvelope({ id: 'sub1', op: 'subscribe', maxEventSize: -1 }), false);
  assert.strictEqual(isRealtimeEnvelope({ id: 'sub1', op: 'subscribe', maxEventSize: 1.5 }), false);
  assert.strictEqual(isRealtimeEnvelope({ id: 'sub1', op: 'subscribe', maxEventSize: '100' }), false);
  assert.strictEqual(isRealtimeEnvelope({ id: 'sub1', op: 'subscribe', maxEventSize: NaN }), false);
});

test('readBoundedText: reads a body within the limit', async () => {
  const response = responseOfBody('{"result":42}', { 'content-type': 'application/json' });
  const text = await readBoundedText(response, 64);
  assert.strictEqual(text, '{"result":42}');
});

test('readBoundedText: reads exactly max bytes and succeeds', async () => {
  const body = 'x'.repeat(64);
  const response = responseOfBody(body);
  const text = await readBoundedText(response, 64);
  assert.strictEqual(text, body);
});

test('readBoundedText: throws when body exceeds max bytes by one', async () => {
  const body = 'x'.repeat(65);
  const response = responseOfBody(body);
  await assert.rejects(readBoundedText(response, 64), /exceeds 64 bytes/);
});

test('readBoundedText: throws when the first chunk is larger than max+1', async () => {
  const response = new Response(
    new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('x'.repeat(100)));
      },
    }),
    { status: 200 },
  );
  await assert.rejects(readBoundedText(response, 64), /exceeds 64 bytes/);
});

test('readBoundedText: rejects invalid UTF-8 bytes', async () => {
  const response = new Response(
    new Uint8Array([0x80, 0x81, 0x82]),
    { status: 200 },
  );
  await assert.rejects(readBoundedText(response, 64), /Invalid UTF-8/);
});

test('readBoundedText: uses default response body limit', async () => {
  const body = 'x'.repeat(MAX_RESPONSE_BODY_BYTES - 1);
  const response = responseOfBody(body);
  const text = await readBoundedText(response);
  assert.strictEqual(text.length, MAX_RESPONSE_BODY_BYTES - 1);
});
