import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { FetchRealtimeTransport } from '../src/realtime.js';
import { PBVexError } from '../src/errors.js';
import { encodeValue, canonicalJson, hashSha256 } from '@pbvex/protocol';
import type { QueryResult, WatchOptions } from '../src/index.js';

const SESSION_URL = 'http://localhost:8090/api/pbvex/realtime/session';
const CONTROL_URL = 'http://localhost:8090/api/pbvex/realtime/session/subscriptions';
const SESSION_ID = 'a'.repeat(64);

class StreamHarness {
  readonly stream: ReadableStream<Uint8Array>;
  controller!: ReadableStreamController<Uint8Array>;
  private encoder = new TextEncoder();

  constructor() {
    this.stream = new ReadableStream<Uint8Array>({
      start: (c) => {
        this.controller = c;
      },
    });
  }

  push(text: string): void {
    this.controller.enqueue(this.encoder.encode(text));
  }

  close(): void {
    try {
      this.controller.close();
    } catch {
      // Already closed (e.g., after the reader was cancelled).
    }
  }
}

async function subscriptionId(path: string, args: unknown): Promise<string> {
  const encoded = args === undefined ? {} : encodeValue(args as any);
  return hashSha256(`v1:${path}:${canonicalJson(encoded)}`);
}

function sse(data: Record<string, unknown>): string {
  return `data: ${JSON.stringify({ data })}\n\n`;
}

function sseEnvelope(payload: unknown, id: string): string {
  return sse({ id, op: 'message', payload });
}

const sseSession = sse({ id: SESSION_ID, op: 'session' });

function eventStream(body: BodyInit): Response {
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

function wait(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

describe('FetchRealtimeTransport', () => {
  it('uses the browser global as the fetch receiver', async () => {
    const browserFetch = function (this: unknown) {
      expect(this).toBe(globalThis);
      return Promise.resolve(new Response(null, { status: 204 }));
    } as typeof globalThis.fetch;
    const transport = new FetchRealtimeTransport({
      baseUrl: 'http://localhost:8090',
      fetch: browserFetch,
    });

    await expect(transport.fetchFn('http://localhost:8090/api/health')).resolves.toHaveProperty('status', 204);
  });

  let fetch: ReturnType<typeof vi.fn>;
  let transports: FetchRealtimeTransport[];

  beforeEach(() => {
    fetch = vi.fn();
    transports = [];
  });

  afterEach(() => {
    transports.forEach((t) => t.close());
  });

  /**
   * Routes session opens to `openSession` and acknowledges every control
   * request, so tests only describe the stream.
   */
  function serve(openSession: (init: RequestInit) => Response | Promise<Response>): void {
    fetch.mockImplementation((input: RequestInfo | URL, init: RequestInit) => {
      const url = input.toString();
      if (url === CONTROL_URL) return new Response(null, { status: 204 });
      if (url === SESSION_URL) return openSession(init);
      throw new Error(`unexpected url ${url}`);
    });
  }

  /** Serves one announced session stream per open. */
  function serveHarnesses(): StreamHarness[] {
    const harnesses: StreamHarness[] = [];
    serve(() => {
      const harness = new StreamHarness();
      harnesses.push(harness);
      harness.push(sseSession);
      return eventStream(harness.stream);
    });
    return harnesses;
  }

  function sessionOpens(): unknown[][] {
    return fetch.mock.calls.filter(([url]) => url.toString() === SESSION_URL);
  }

  function makeTransport(
    auth?: (() => string | Promise<string | undefined> | undefined) | undefined,
    opts: { maxReconnects?: number; initialReconnectDelayMs?: number; maxReconnectDelayMs?: number; timeoutMs?: number; limits?: { maxFunctionArgsBytes?: number; maxReturnValueBytes?: number } } = {},
  ): FetchRealtimeTransport {
    const transport = new FetchRealtimeTransport({
      baseUrl: 'http://localhost:8090',
      fetch: fetch as unknown as typeof globalThis.fetch,
      getAuthToken: auth,
      ...opts,
    });
    transports.push(transport);
    return transport;
  }

  function watchUpdates(transport: FetchRealtimeTransport, path = 'messages:list', args: unknown = { userId: 'u1' }) {
    const updates: QueryResult<unknown>[] = [];
    const errors: Error[] = [];
    const unsubscribe = transport.watch(path, args, {
      onUpdate: (result) => updates.push(result),
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);
    const has = (value: unknown) => updates.some((u) => JSON.stringify(u.data) === JSON.stringify(value));
    return { updates, errors, unsubscribe, has };
  }

  it('does not put the auth token in the realtime URL', async () => {
    serveHarnesses();
    const transport = makeTransport(() => 'secret-token');
    watchUpdates(transport);
    await vi.waitFor(() => expect(fetch.mock.calls.some(([url]) => url === CONTROL_URL)).toBe(true));

    for (const [input, init] of fetch.mock.calls as [string, RequestInit][]) {
      const url = new URL(input);
      expect(url.searchParams.toString()).toBe('');
      expect(url.toString()).not.toContain('secret');
      expect(init.method).toBe('POST');
      expect((init.headers as Record<string, string>).Authorization).toBe('Bearer secret-token');
    }
    expect(fetch.mock.calls.map(([url]) => url)).toEqual([SESSION_URL, CONTROL_URL]);
  });

  it('subscribes with the subscription id, path, and encoded args', async () => {
    serveHarnesses();
    const transport = makeTransport();
    watchUpdates(transport);
    await vi.waitFor(() => expect(fetch.mock.calls.some(([url]) => url === CONTROL_URL)).toBe(true));

    const control = fetch.mock.calls.find(([url]) => url === CONTROL_URL)!;
    const init = control[1] as RequestInit;
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json');
    expect(JSON.parse(init.body as string)).toEqual({
      session: SESSION_ID,
      subscribe: [{ id: await subscriptionId('messages:list', { userId: 'u1' }), path: 'messages:list', args: { userId: 'u1' } }],
      unsubscribe: [],
    });
    const open = sessionOpens()[0]![1] as RequestInit;
    expect((open.headers as Record<string, string>).Accept).toBe('text/event-stream');
  });

  it('parses SSE events split across multiple chunks', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(10);

    const event = sseEnvelope(encodeValue({ value: 42 }), id);
    harnesses[0]!.push(event.slice(0, 10));
    await wait(5);
    harnesses[0]!.push(event.slice(10));
    await wait(10);

    expect(watch.has({ value: 42 })).toBe(true);
  });

  it('replays the latest result to watchers joining after the initial message', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    watchUpdates(transport);
    await wait(10);
    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 7 }), id));
    await wait(10);

    const late = watchUpdates(transport);
    await wait(10);

    expect(sessionOpens()).toHaveLength(1);
    expect(late.updates.at(-1)?.data).toEqual({ value: 7 });
  });

  it('reports malformed SSE data and continues to parse later events', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(10);

    harnesses[0]!.push('data: not-json\n\n');
    await wait(5);
    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 5 }), id));
    await wait(10);

    expect(watch.errors.length).toBeGreaterThan(0);
    expect(watch.has({ value: 5 })).toBe(true);
  });

  it('ignores SSE events with unknown subscription ids', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(10);

    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 1 }), 'other:subscription'));
    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 2 }), id));
    await wait(10);

    expect(watch.has({ value: 1 })).toBe(false);
    expect(watch.has({ value: 2 })).toBe(true);
  });

  it('ignores comments, heartbeat blank records, ping events, and CRLF framing', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(10);

    harnesses[0]!.push(':this is a comment\n:another comment\n\n');
    harnesses[0]!.push(sse({ id: SESSION_ID, op: 'ping' }));
    harnesses[0]!.push(`data: ${JSON.stringify({ data: { id, op: 'message', payload: encodeValue({ value: 'crlf' }) } })}\r\n\r\n`);
    await wait(10);

    expect(watch.errors).toEqual([]);
    expect(watch.has({ value: 'crlf' })).toBe(true);
  });

  it('reports structured errors delivered as payloads', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(10);

    harnesses[0]!.push(sseEnvelope({ error: true, code: 'forbidden', message: 'nope', details: [] }, id));
    await wait(10);

    expect(watch.updates.at(-1)?.error).toBeInstanceOf(PBVexError);
    expect(watch.errors[0]).toBeInstanceOf(PBVexError);
  });

  it('parses structured errors from failed HTTP responses', async () => {
    serve(() => new Response(
      JSON.stringify({ error: true, code: 'unauthorized', message: 'bad token' }),
      { status: 401, headers: { 'Content-Type': 'application/json' } },
    ));
    const transport = makeTransport(undefined, { maxReconnects: 0 });
    const watch = watchUpdates(transport);
    await wait(50);

    expect(watch.errors[0]).toBeInstanceOf(PBVexError);
    expect(watch.errors[0]!.message).toBe('bad token');
  });

  it('rejects a non-SSE response and aborts its request', async () => {
    let signal: AbortSignal | null | undefined;
    serve((init) => {
      signal = init.signal;
      return new Response(new ReadableStream({ start: (c) => c.enqueue(new TextEncoder().encode('nope')) }), {
        status: 200,
        headers: { 'Content-Type': 'text/plain' },
      });
    });
    const transport = makeTransport(undefined, { maxReconnects: 0 });
    const watch = watchUpdates(transport);
    await wait(50);

    expect(watch.errors.some((e) => e.message.includes('content-type'))).toBe(true);
    expect(signal?.aborted).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
    expect(sessionOpens()).toHaveLength(1);
  });

  it('exhausts maxReconnects on streams that end immediately', async () => {
    serve(() => eventStream(new ReadableStream({ start: (c) => c.close() })));
    const transport = makeTransport(undefined, { maxReconnects: 2, initialReconnectDelayMs: 5 });
    const watch = watchUpdates(transport);
    await wait(200);

    expect(watch.errors.some((e) => e.message.includes('reconnect limit'))).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
    expect(sessionOpens()).toHaveLength(3);
  });

  it('does not reset reconnect attempts on control events or undecodable messages', async () => {
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    serve(() => {
      const harness = new StreamHarness();
      harness.push(sseSession);
      harness.push(sse({ id, op: 'subscribe', maxEventSize: 1 << 20 }));
      harness.push(sseEnvelope({ $bogus: true }, id));
      harness.close();
      return eventStream(harness.stream);
    });
    const transport = makeTransport(undefined, { maxReconnects: 1, initialReconnectDelayMs: 5 });
    const watch = watchUpdates(transport);
    await wait(200);

    // A reset counter would reconnect forever; 1 attempt + 1 reconnect is 2.
    expect(watch.errors.some((e) => e.message.includes('reconnect limit'))).toBe(true);
    expect(sessionOpens()).toHaveLength(2);
  });

  it('opens a fresh session for a watch after the reconnect limit', async () => {
    serve(() => eventStream(new ReadableStream({ start: (c) => c.close() })));
    const transport = makeTransport(undefined, { maxReconnects: 0 });
    watchUpdates(transport);
    await wait(50);
    expect(sessionOpens()).toHaveLength(1);

    const harnesses = serveHarnesses();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(20);
    harnesses[0]!.push(sseEnvelope(encodeValue('back'), id));
    await wait(10);

    expect(watch.has('back')).toBe(true);
  });

  it('cancels the reader and aborts when the stream errors mid-read', async () => {
    let signal: AbortSignal | null | undefined;
    serve((init) => {
      signal = init.signal;
      return eventStream(new ReadableStream({ start: (c) => c.error(new Error('socket reset')) }));
    });
    const transport = makeTransport(undefined, { maxReconnects: 0 });
    const watch = watchUpdates(transport);
    await wait(50);

    expect(watch.errors.some((e) => /socket reset/.test(e.message))).toBe(true);
    expect(signal?.aborted).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
  });

  it('reassembles events split across multibyte UTF-8 chunk boundaries', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(10);

    const bytes = new TextEncoder().encode(sseEnvelope(encodeValue({ value: '🙂' }), id));
    const mid = Math.floor(bytes.length / 2);
    harnesses[0]!.controller.enqueue(bytes.subarray(0, mid));
    await wait(5);
    harnesses[0]!.controller.enqueue(bytes.subarray(mid));
    await wait(10);

    expect(watch.has({ value: '🙂' })).toBe(true);
  });

  it('treats truncated UTF-8 at EOF as a fatal stream error', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport(undefined, { maxReconnects: 0 });
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(10);

    const event = sseEnvelope(encodeValue({ value: '🙂' }), id);
    const prefix = new TextEncoder().encode(event.slice(0, event.indexOf('🙂')));
    // Split inside the 4-byte emoji so the decoder holds a truncated sequence at EOF.
    harnesses[0]!.controller.enqueue(new TextEncoder().encode(event).subarray(0, prefix.length + 1));
    harnesses[0]!.close();
    await wait(50);

    expect(watch.errors.some((e) => e.message.includes('Invalid UTF-8'))).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
  });

  it('discards oversized SSE lines and continues processing', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(10);

    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 'a'.repeat(1052672 + 1000) }), id));
    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 'ok' }), id));
    await wait(10);

    expect(watch.errors.some((e) => e.message.includes('maximum length'))).toBe(true);
    expect(watch.has({ value: 'ok' })).toBe(true);
  });

  it('enforces a configured maxReturnValueBytes on SSE event data', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport(undefined, { limits: { maxReturnValueBytes: 64 } });
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(10);

    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 'a'.repeat(5000) }), id));
    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 'ok' }), id));
    await wait(10);

    expect(watch.errors.some((e) => e.message.includes('maximum length'))).toBe(true);
    expect(watch.has({ value: 'ok' })).toBe(true);
  });

  it('applies a server-negotiated maxEventSize bounded by the client ceiling', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport(undefined, { limits: { maxReturnValueBytes: 10000 } });
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(10);

    harnesses[0]!.push(sse({ id, op: 'subscribe', maxEventSize: 200 }));
    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 'a'.repeat(300) }), id));
    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 'ok' }), id));
    await wait(10);

    expect(watch.errors.some((e) => e.message.includes('maximum length'))).toBe(true);
    expect(watch.has({ value: 'ok' })).toBe(true);
  });

  it('does not loosen the client ceiling when the server advertises a larger maxEventSize', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport(undefined, { limits: { maxReturnValueBytes: 64 } });
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(10);

    harnesses[0]!.push(sse({ id, op: 'subscribe', maxEventSize: 999999999 }));
    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 'a'.repeat(5000) }), id));
    await wait(10);

    expect(watch.errors.some((e) => e.message.includes('maximum length'))).toBe(true);
  });

  it('does not break the stream when a watcher callback throws', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {
        throw new Error('boom');
      },
    } as WatchOptions<unknown>);
    const watch = watchUpdates(transport);
    await wait(10);

    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 1 }), id));
    harnesses[0]!.push(sseEnvelope(encodeValue({ value: 2 }), id));
    await wait(10);

    expect(watch.has({ value: 1 })).toBe(true);
    expect(watch.has({ value: 2 })).toBe(true);
  });

  it('rejects oversized realtime args and paths', () => {
    const transport = makeTransport();
    expect(() => transport.watch('messages:list', { data: 'a'.repeat(1024 * 1024 + 1) }, {
      onUpdate: () => {},
    } as WatchOptions<unknown>)).toThrow('Realtime subscription args exceed');
    expect(() => transport.watch('a'.repeat(5000), {}, {
      onUpdate: () => {},
    } as WatchOptions<unknown>)).toThrow('Realtime path exceeds');
  });

  it('validates configurable transport limits', () => {
    expect(() => makeTransport(undefined, { limits: { maxReturnValueBytes: -1 } })).toThrow(/non-negative integer/);
    expect(() => makeTransport(undefined, { timeoutMs: 0 })).toThrow(/positive integer/);
    expect(() => makeTransport(undefined, { initialReconnectDelayMs: 100, maxReconnectDelayMs: 50 })).toThrow(/maxReconnectDelayMs/);
  });

  it('close is idempotent and stops new subscriptions', async () => {
    serveHarnesses();
    const transport = makeTransport();
    const watch = watchUpdates(transport);
    await wait(10);

    transport.close();
    transport.close();
    expect(transport.connectionState).toBe('disconnected');

    const noop = transport.watch('messages:list', { userId: 'u2' }, { onUpdate: () => {} } as WatchOptions<unknown>);
    noop();
    await wait(10);
    expect(sessionOpens()).toHaveLength(1);
    watch.unsubscribe();
  });

  it('aborts a hung getAuthToken on close without opening a stream', async () => {
    let resolveAuth: ((value: string) => void) | undefined;
    const auth = vi.fn(() => new Promise<string>((resolve) => { resolveAuth = resolve; }));
    serveHarnesses();
    const transport = makeTransport(auth, { maxReconnects: 0 });
    watchUpdates(transport);
    await wait(20);
    expect(auth).toHaveBeenCalled();

    transport.close();
    await wait(20);
    expect(transport.connectionState).toBe('disconnected');

    resolveAuth!('late-token');
    await wait(20);
    expect(fetch).not.toHaveBeenCalled();
  });

  it('aborts a hung getAuthToken on refreshAuth and uses the new token', async () => {
    let resolveAuth: ((value: string | undefined) => void) | undefined;
    let calls = 0;
    const auth = vi.fn(() => {
      calls += 1;
      if (calls === 1) return new Promise<string | undefined>((resolve) => { resolveAuth = resolve; });
      return Promise.resolve('token-b');
    });
    serveHarnesses();
    const transport = makeTransport(auth, { maxReconnects: 0 });
    watchUpdates(transport);
    await wait(20);

    transport.refreshAuth();
    await wait(20);
    expect(sessionOpens()).toHaveLength(1);
    expect((sessionOpens()[0]![1] as RequestInit & { headers: Record<string, string> }).headers.Authorization).toBe('Bearer token-b');

    resolveAuth!('stale-token');
    await wait(20);
    expect(sessionOpens()).toHaveLength(1);
  });

  it('times out a hung getAuthToken and reports the error', async () => {
    serveHarnesses();
    const transport = makeTransport(() => new Promise<string>(() => {}), { maxReconnects: 0, timeoutMs: 30 });
    const watch = watchUpdates(transport);
    await wait(120);

    expect(watch.errors.some((e) => /timeout/i.test(e.message))).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
  });

  it('times out a session open that hangs before response headers', async () => {
    let signal: AbortSignal | null | undefined;
    serve((init) => {
      signal = init.signal;
      return new Promise<Response>((_, reject) => {
        init.signal?.addEventListener('abort', () => reject(new Error('AbortError')));
      });
    });
    const transport = makeTransport(undefined, { maxReconnects: 0, timeoutMs: 40 });
    const watch = watchUpdates(transport);
    await wait(150);

    expect(watch.errors.some((e) => /timeout/i.test(e.message))).toBe(true);
    expect(signal?.aborted).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
  });

  it('does not abort an established stream after the open deadline', async () => {
    const harnesses = serveHarnesses();
    const transport = makeTransport(undefined, { timeoutMs: 20 });
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const watch = watchUpdates(transport);
    await wait(80);

    harnesses[0]!.push(sseEnvelope(encodeValue('still-open'), id));
    await wait(10);

    expect(sessionOpens()).toHaveLength(1);
    expect(watch.has('still-open')).toBe(true);
  });
});
