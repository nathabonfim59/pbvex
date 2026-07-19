import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { FetchRealtimeTransport } from '../src/realtime.js';
import { PBVexError } from '../src/errors.js';
import { encodeValue, canonicalJson, hashSha256 } from '@pbvex/protocol';
import type { ConnectionState, QueryResult, WatchOptions } from '../src/index.js';

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
      // Already closed (e.g., after reader.cancel() during refreshAuth).
    }
  }
}

async function subscriptionId(path: string, args: unknown): Promise<string> {
  const encoded = args === undefined ? {} : encodeValue(args as any);
  return hashSha256(`v1:${path}:${canonicalJson(encoded)}`);
}

function sseEnvelope(payload: unknown, id: string): string {
  return `data: ${JSON.stringify({ data: { id, op: 'message', payload } })}

`;
}

function sseSubscribe(id: string): string {
  return `data: ${JSON.stringify({ data: { id, op: 'subscribe' } })}

`;
}

function ssePing(id: string): string {
  return `data: ${JSON.stringify({ data: { id, op: 'ping' } })}

`;
}

function wait(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function collectStates(): { states: ConnectionState[]; push: (state: ConnectionState) => void } {
  const states: ConnectionState[] = [];
  return {
    states,
    push: (state: ConnectionState) => states.push(state),
  };
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

  it('streams SSE events and routes them by subscription id', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const updates: QueryResult<unknown>[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const unsubscribe = transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updates.push(result),
    } as WatchOptions<unknown>);

    await wait(10);
    harness.push(sseEnvelope(encodeValue({ value: 1 }), id));
    await wait(10);

    expect(updates.length).toBeGreaterThanOrEqual(2);
    expect(updates[updates.length - 1].data).toEqual({ value: 1 });
    expect(updates[updates.length - 1].error).toBeNull();

    unsubscribe();
  });

  it('parses SSE events split across multiple chunks', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const updates: QueryResult<unknown>[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updates.push(result),
    } as WatchOptions<unknown>);

    await wait(10);
    const event = sseEnvelope(encodeValue({ value: 2 }), id);
    harness.push(event.slice(0, event.indexOf('{') + 3));
    await wait(5);
    harness.push(event.slice(event.indexOf('{') + 3));
    await wait(10);

    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 2 }))).toBe(true);
  });

  it('does not put auth token in the realtime URL', async () => {
    const harness = new StreamHarness();
    let capturedUrl: URL | undefined;
    fetch.mockImplementation((input: RequestInfo | URL) => {
      capturedUrl = new URL(input.toString());
      return new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
    });

    const transport = makeTransport(() => 'secret-token');
    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
    } as WatchOptions<unknown>);

    await wait(10);

    expect(capturedUrl).toBeDefined();
    expect(capturedUrl!.searchParams.toString()).toBe('');
    expect(capturedUrl!.toString()).not.toContain('secret');

    const init = fetch.mock.calls[0][1] as RequestInit | undefined;
    expect(init?.method).toBe('POST');
    expect((init?.headers as Record<string, string> | undefined)?.Authorization).toBe('Bearer secret-token');
  });

  it('does not run an initial HTTP query; the backend SSE is authoritative for initial results', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const updatesA: QueryResult<unknown>[] = [];
    const updatesB: QueryResult<unknown>[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updatesA.push(result),
    } as WatchOptions<unknown>);
    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updatesB.push(result),
    } as WatchOptions<unknown>);

    await wait(10);

    expect(fetch).toHaveBeenCalledTimes(1);

    expect(updatesA[0].isLoading).toBe(true);
    expect(updatesB[0].isLoading).toBe(true);

    harness.push(sseEnvelope(encodeValue({ initial: true }), id));
    await wait(10);

    expect(updatesA[updatesA.length - 1].data).toEqual({ initial: true });
    expect(updatesB[updatesB.length - 1].data).toEqual({ initial: true });

    harness.push(sseEnvelope(encodeValue({ value: 3 }), id));
    await wait(10);

    expect(updatesA.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 3 }))).toBe(true);
    expect(updatesB.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 3 }))).toBe(true);
  });

  it('reconnects with bounded retries and reports state changes', async () => {
    let callCount = 0;
    const harnesses: StreamHarness[] = [];
    fetch.mockImplementation(() => {
      callCount += 1;
      const harness = new StreamHarness();
      harnesses.push(harness);
      if (callCount === 1) {
        return new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
      }
      return new Response('error', { status: 500 });
    });

    const states = collectStates();
    const errors: Error[] = [];
    const transport = makeTransport(undefined, { maxReconnects: 1, initialReconnectDelayMs: 10 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (error) => errors.push(error),
      onConnectionStateChange: states.push,
    } as WatchOptions<unknown>);

    await wait(20);
    harnesses[0].close();
    await wait(200);

    expect(states.states).toContain('connecting');
    expect(states.states).toContain('connected');
    expect(states.states).toContain('reconnecting');
    expect(errors.length).toBeGreaterThan(0);
  });

  it('unsubscribes and cleans up connections', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const transport = makeTransport();
    const unsubscribe = transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
    } as WatchOptions<unknown>);

    await wait(10);
    expect(transport.connectionState).not.toBe('disconnected');

    unsubscribe();
    await wait(10);

    expect(transport.connectionState).toBe('disconnected');
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('reports structured errors delivered as payloads', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const updates: QueryResult<unknown>[] = [];
    const errors: Error[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updates.push(result),
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(10);
    const payload = { error: true, code: 'conflict', message: 'Conflict.', data: { retryAfter: { $integer: 'AQAAAAAAAAA=' } } };
    harness.push(sseEnvelope(payload, id));
    await wait(10);

    const last = updates[updates.length - 1];
    expect(last.error).toBeInstanceOf(PBVexError);
    expect(last.error?.message).toBe('Conflict.');
    expect((last.error as PBVexError).data).toEqual({ retryAfter: 1n });
    expect(errors[0]).toBeInstanceOf(PBVexError);
  });

  it('normalizes a rejected auth provider through onError and bounded retry', async () => {
    const auth = vi.fn(() => Promise.reject('auth provider failed'));
    const errors: Error[] = [];
    const states: ConnectionState[] = [];
    const transport = makeTransport(auth, { maxReconnects: 0 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (error) => errors.push(error),
      onConnectionStateChange: (state) => states.push(state),
    } as WatchOptions<unknown>);

    await wait(50);

    expect(auth).toHaveBeenCalled();
    expect(fetch).not.toHaveBeenCalled();
    expect(errors.some((e) => e.message === 'auth provider failed')).toBe(true);
    expect(states).toContain('reconnecting');
    expect(transport.connectionState).toBe('disconnected');
  });

  it('reconnects bounded without rerunning an initial HTTP query', async () => {
    let callCount = 0;
    const harnesses: StreamHarness[] = [];
    fetch.mockImplementation(() => {
      callCount += 1;
      const harness = new StreamHarness();
      harnesses.push(harness);
      if (callCount === 1) {
        return new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
      }
      return new Response('error', { status: 500 });
    });

    const transport = makeTransport(undefined, { maxReconnects: 1, initialReconnectDelayMs: 10 });
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
    } as WatchOptions<unknown>);

    await wait(20);
    harnesses[0].close();
    await wait(200);

    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('retries a failed initial connection and then receives the initial message from SSE', async () => {
    let callCount = 0;
    const harnesses: StreamHarness[] = [];
    fetch.mockImplementation(() => {
      callCount += 1;
      const harness = new StreamHarness();
      harnesses.push(harness);
      if (callCount === 1) {
        return new Response('error', { status: 500 });
      }
      return new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
    });

    const updates: QueryResult<unknown>[] = [];
    const transport = makeTransport(undefined, { maxReconnects: 2, initialReconnectDelayMs: 10 });
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updates.push(result),
    } as WatchOptions<unknown>);

    await wait(200);

    expect(fetch).toHaveBeenCalledTimes(2);

    harnesses[1].push(sseEnvelope(encodeValue({ initial: true }), id));
    await wait(10);

    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ initial: true }))).toBe(true);
  });

  it('applies per-subscription reconnect settings from WatchOptions', async () => {
    fetch.mockImplementation(() => new Response('error', { status: 500 }));

    const transport = makeTransport(undefined, { maxReconnects: 5, initialReconnectDelayMs: 1000 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      maxReconnects: 0,
      initialReconnectDelayMs: 10,
    } as WatchOptions<unknown>);

    await wait(50);

    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('replays the latest result to watchers joining after the initial message', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const updatesA: QueryResult<unknown>[] = [];
    const updatesB: QueryResult<unknown>[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updatesA.push(result),
    } as WatchOptions<unknown>);

    await wait(10);
    harness.push(sseEnvelope(encodeValue({ value: 7 }), id));
    await wait(10);

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updatesB.push(result),
    } as WatchOptions<unknown>);

    await wait(10);

    expect(fetch).toHaveBeenCalledTimes(1);
    expect(updatesB[updatesB.length - 1].data).toEqual({ value: 7 });
  });

  it('does not overwrite a message with a stale result', async () => {
    const harness = new StreamHarness();
    let releaseHandshake: () => void;
    const handshake = new Promise<void>((resolve) => {
      releaseHandshake = resolve;
    });
    fetch.mockImplementation(async () => {
      await handshake;
      return new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
    });

    const updates: QueryResult<unknown>[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updates.push(result),
    } as WatchOptions<unknown>);

    await wait(50);
    expect(fetch).toHaveBeenCalledTimes(1);

    releaseHandshake!();
    await wait(10);
    harness.push(sseEnvelope(encodeValue({ value: 'from-sse' }), id));
    await wait(150);

    expect(fetch).toHaveBeenCalledTimes(1);
    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 'from-sse' }))).toBe(true);
    expect(updates[updates.length - 1].data).toEqual({ value: 'from-sse' });
  });

  it('reports malformed SSE data and continues to parse later events', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const updates: QueryResult<unknown>[] = [];
    const errors: Error[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updates.push(result),
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(10);
    harness.push(`data: not-json\n\n`);
    await wait(5);
    harness.push(sseEnvelope(encodeValue({ value: 5 }), id));
    await wait(10);

    expect(errors.length).toBeGreaterThan(0);
    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 5 }))).toBe(true);
  });

  it('ignores SSE events with unknown subscription ids', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const updates: QueryResult<unknown>[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updates.push(result),
    } as WatchOptions<unknown>);

    await wait(10);
    harness.push(sseEnvelope(encodeValue({ value: 1 }), 'other:subscription'));
    await wait(5);
    harness.push(sseEnvelope(encodeValue({ value: 2 }), id));
    await wait(10);

    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 1 }))).toBe(false);
    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 2 }))).toBe(true);
  });

  it('ignores comments, heartbeat blank records, and CRLF framing', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const updates: QueryResult<unknown>[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updates.push(result),
    } as WatchOptions<unknown>);

    await wait(10);
    harness.push(`:this is a comment\n:another comment\n\n`);
    harness.push(`data: ${JSON.stringify({ data: { id, op: 'message', payload: encodeValue({ value: 'crlf' }) } })}\r\n\r\n`);
    await wait(10);

    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 'crlf' }))).toBe(true);
  });

  it('handles ping/subscribe/unsubscribe control events as keepalive', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const updates: QueryResult<unknown>[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updates.push(result),
    } as WatchOptions<unknown>);

    await wait(10);
    harness.push(ssePing(id));
    harness.push(sseSubscribe(id));
    harness.push(sseEnvelope(encodeValue({ value: 'ok' }), id));
    await wait(10);

    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 'ok' }))).toBe(true);
  });

  it('rejects non-SSE responses and reconnects', async () => {
    fetch.mockImplementation(() => new Response('not an event stream', { status: 200, headers: { 'Content-Type': 'text/plain' } }));

    const errors: Error[] = [];
    const transport = makeTransport(undefined, { maxReconnects: 0, initialReconnectDelayMs: 10 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(50);

    expect(errors.length).toBeGreaterThan(0);
  });

  it('exhausts maxReconnects on repeated valid SSE headers with immediate EOF and disposes', async () => {
    fetch.mockImplementation(() => new Response(new ReadableStream({ start: (c) => c.close() }), { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const errors: Error[] = [];
    const transport = makeTransport(undefined, { maxReconnects: 2, initialReconnectDelayMs: 5 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(200);

    expect(errors.some((e) => e.message.includes('reconnect limit'))).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
    expect(fetch).toHaveBeenCalledTimes(3);
  });

  it('parses structured errors from failed HTTP responses', async () => {
    fetch.mockImplementation(() => new Response(
      JSON.stringify({ error: true, code: 'unauthorized', message: 'bad token' }),
      { status: 401, headers: { 'Content-Type': 'application/json' } },
    ));

    const errors: Error[] = [];
    const transport = makeTransport(undefined, { maxReconnects: 0 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(50);

    expect(errors[0]).toBeInstanceOf(PBVexError);
    expect(errors[0].message).toBe('bad token');
  });

  it('refreshAuth reconnects live subscriptions with a new token', async () => {
    const harnesses: StreamHarness[] = [];
    let token = 'token-a';
    fetch.mockImplementation(() => {
      const harness = new StreamHarness();
      harnesses.push(harness);
      return new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
    });

    const transport = makeTransport(() => token);
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
    } as WatchOptions<unknown>);

    await wait(10);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][1].headers.Authorization).toBe('Bearer token-a');

    token = 'token-b';
    transport.refreshAuth();
    await wait(10);

    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch.mock.calls[1][1].headers.Authorization).toBe('Bearer token-b');

    harnesses[0].close();
    await wait(10);
    // No new connection should be scheduled while the old one is closed.
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('close is idempotent and stops new subscriptions', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const transport = makeTransport();
    const unsubscribe = transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
    } as WatchOptions<unknown>);

    await wait(10);
    transport.close();
    transport.close();

    expect(transport.connectionState).toBe('disconnected');

    const noop = transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
    } as WatchOptions<unknown>);
    expect(noop).toBeInstanceOf(Function);
    noop();

    expect(fetch).toHaveBeenCalledTimes(1);
    unsubscribe();
  });

  it('sends a POST body with subscription id, path, and encoded args', async () => {
    const harness = new StreamHarness();
    let body: unknown;
    fetch.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const request = new Request(input, init);
      body = request.json();
      return new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
    });

    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
    } as WatchOptions<unknown>);

    await wait(10);

    await expect(body).resolves.toEqual({
      id,
      path: 'messages:list',
      args: { userId: 'u1' },
    });
    const request = new Request('http://localhost:8090/api/pbvex/realtime', fetch.mock.calls[0][1] as RequestInit);
    expect(request.method).toBe('POST');
    expect(request.headers.get('content-type')).toBe('application/json');
    expect(request.headers.get('accept')).toBe('text/event-stream');
  });

  it('rejects oversized realtime args', () => {
    const transport = makeTransport();
    expect(() => transport.watch('messages:list', { data: 'a'.repeat(1024 * 1024 + 1) }, {
      onUpdate: () => {},
    } as WatchOptions<unknown>)).toThrow('Realtime subscription args exceed');
  });

  it('rejects oversized realtime paths', () => {
    const transport = makeTransport();
    expect(() => transport.watch('a'.repeat(5000), {}, {
      onUpdate: () => {},
    } as WatchOptions<unknown>)).toThrow('Realtime path exceeds');
  });

  it('reassembles events split across multibyte UTF-8 chunk boundaries', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const updates: QueryResult<unknown>[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updates.push(result),
    } as WatchOptions<unknown>);

    await wait(10);
    const event = sseEnvelope(encodeValue({ value: '🙂' }), id);
    const bytes = new TextEncoder().encode(event);
    const mid = Math.floor(bytes.length / 2);
    harness.controller.enqueue(bytes.subarray(0, mid));
    await wait(5);
    harness.controller.enqueue(bytes.subarray(mid));
    await wait(10);

    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: '🙂' }))).toBe(true);
  });

  it('treats truncated UTF-8 at EOF as a fatal error and stops when maxReconnects is 0', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const errors: Error[] = [];
    const transport = makeTransport(undefined, { maxReconnects: 0, initialReconnectDelayMs: 10 });
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(10);
    const event = sseEnvelope(encodeValue({ value: '🙂' }), id);
    const emojiPos = event.indexOf('🙂');
    const prefixBytes = new TextEncoder().encode(event.slice(0, emojiPos));
    const bytes = new TextEncoder().encode(event);
    // Split inside the 4-byte emoji so the decoder is left with a truncated
    // UTF-8 sequence at EOF.
    harness.controller.enqueue(bytes.subarray(0, prefixBytes.length + 1));
    harness.close();
    await wait(50);

    expect(errors.some((e) => e.message.includes('Invalid UTF-8'))).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
  });

  it('discards oversized SSE lines and continues processing', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const errors: Error[] = [];
    const updates: QueryResult<unknown>[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updates.push(result),
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(10);
    const oversizedPayload = 'a'.repeat(1052672 + 1000);
    harness.push(sseEnvelope(encodeValue({ value: oversizedPayload }), id));
    harness.push(sseEnvelope(encodeValue({ value: 'ok' }), id));
    await wait(10);

    expect(errors.some((e) => e.message.includes('maximum length'))).toBe(true);
    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 'ok' }))).toBe(true);
  });

  it('does not break the stream when a watcher callback throws', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const updates: QueryResult<unknown>[] = [];
    const transport = makeTransport();
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => {
        if (updates.length === 0) {
          throw new Error('boom');
        }
        updates.push(result);
      },
    } as WatchOptions<unknown>);
    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (result) => updates.push(result),
    } as WatchOptions<unknown>);

    await wait(10);
    harness.push(sseEnvelope(encodeValue({ value: 1 }), id));
    harness.push(sseEnvelope(encodeValue({ value: 2 }), id));
    await wait(10);

    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 1 }))).toBe(true);
    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 2 }))).toBe(true);
  });

  it('stops reconnecting after maxReconnects', async () => {
    fetch.mockImplementation(() => new Response('not an event stream', { status: 200, headers: { 'Content-Type': 'text/plain' } }));

    const errors: Error[] = [];
    const transport = makeTransport(undefined, { maxReconnects: 0, initialReconnectDelayMs: 10 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(50);

    expect(errors.length).toBeGreaterThan(0);
    expect(transport.connectionState).toBe('disconnected');
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('a subscribe control envelope does not reset reconnect attempts before EOF', async () => {
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const encoder = new TextEncoder();
    let callCount = 0;
    fetch.mockImplementation(() => {
      callCount += 1;
      const stream = new ReadableStream({
        start(controller) {
          controller.enqueue(encoder.encode(sseSubscribe(id)));
          controller.close();
        },
      });
      return new Response(stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
    });

    const errors: Error[] = [];
    const transport = makeTransport(undefined, { maxReconnects: 1, initialReconnectDelayMs: 5 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(200);

    // If subscribe reset the counter, this would reconnect forever and never
    // reach the limit. Exactly 1 initial attempt + 1 reconnect = 2 fetches.
    expect(errors.some((e) => e.message.includes('reconnect limit'))).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('aborts the in-flight request immediately on a non-SSE content type', async () => {
    let capturedSignal: AbortSignal | null | undefined;
    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('not an event stream'));
      },
    });
    fetch.mockImplementation((_input: RequestInfo | URL, init?: RequestInit) => {
      capturedSignal = init?.signal;
      return new Response(stream, { status: 200, headers: { 'Content-Type': 'text/plain' } });
    });

    const errors: Error[] = [];
    const transport = makeTransport(undefined, { maxReconnects: 0, initialReconnectDelayMs: 10 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(50);

    expect(errors.some((e) => e.message.includes('content-type'))).toBe(true);
    expect(capturedSignal?.aborted).toBe(true);
  });

  it('cancels the reader and aborts when the stream errors mid-read', async () => {
    let capturedSignal: AbortSignal | null | undefined;
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.error(new Error('socket reset'));
      },
    });
    fetch.mockImplementation((_input: RequestInfo | URL, init?: RequestInit) => {
      capturedSignal = init?.signal;
      return new Response(stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
    });

    const errors: Error[] = [];
    const transport = makeTransport(undefined, { maxReconnects: 0, initialReconnectDelayMs: 10 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(50);

    expect(errors.some((e) => /socket reset|stream|realtime/i.test(e.message))).toBe(true);
    expect(capturedSignal?.aborted).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
  });

  it('aborts a hung getAuthToken on close without leaking a pending generation', async () => {
    let resolveAuth: ((value: string) => void) | undefined;
    const auth = vi.fn(() => new Promise<string>((resolve) => { resolveAuth = resolve; }));
    fetch.mockImplementation(() => new Response(new StreamHarness().stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const transport = makeTransport(auth, { maxReconnects: 0 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
    } as WatchOptions<unknown>);

    await wait(20);
    expect(auth).toHaveBeenCalled();
    expect(fetch).not.toHaveBeenCalled();

    // Auth is hung; closing must abort it without resurrecting the connection.
    transport.close();
    await wait(20);
    expect(transport.connectionState).toBe('disconnected');

    // A late auth resolution must not resurrect the disposed transport.
    resolveAuth!('late-token');
    await wait(20);
    expect(fetch).not.toHaveBeenCalled();
  });

  it('aborts a hung getAuthToken on refreshAuth and uses the new token', async () => {
    let resolveAuth: ((value: string | undefined) => void) | undefined;
    let callCount = 0;
    const auth = vi.fn(() => {
      callCount += 1;
      if (callCount === 1) {
        return new Promise<string | undefined>((resolve) => { resolveAuth = resolve; });
      }
      return Promise.resolve('token-b');
    });

    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const transport = makeTransport(auth, { maxReconnects: 0 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
    } as WatchOptions<unknown>);

    await wait(20);
    expect(auth).toHaveBeenCalledTimes(1);

    // refreshAuth aborts the hung first auth and opens a new connection.
    transport.refreshAuth();
    await wait(20);

    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][1].headers.Authorization).toBe('Bearer token-b');

    // The stale auth resolution must not produce a second connection.
    resolveAuth!('stale-token');
    await wait(20);
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('times out a hung getAuthToken and reports the error', async () => {
    const auth = vi.fn(() => new Promise<string>(() => {}));
    fetch.mockImplementation(() => new Response(new StreamHarness().stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const errors: Error[] = [];
    const transport = makeTransport(auth, { maxReconnects: 0, timeoutMs: 30 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(120);

    expect(errors.some((e) => /timeout/i.test(e.message))).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
  });

  it('enforces a configured maxReturnValueBytes on SSE event data', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const errors: Error[] = [];
    const transport = makeTransport(undefined, { limits: { maxReturnValueBytes: 64 } });
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (error) => errors.push(error),
    } as WatchOptions<unknown>);

    await wait(10);
    harness.push(sseEnvelope(encodeValue({ value: 'a'.repeat(5000) }), id));
    harness.push(sseEnvelope(encodeValue({ value: 'ok' }), id));
    await wait(10);

    expect(errors.some((e) => e.message.includes('maximum length'))).toBe(true);
  });

  it('validates configurable transport limits', () => {
    expect(() => new FetchRealtimeTransport({
      baseUrl: 'http://localhost:8090',
      fetch: fetch as unknown as typeof globalThis.fetch,
      limits: { maxReturnValueBytes: -1 },
    })).toThrow(/non-negative integer/);

    expect(() => new FetchRealtimeTransport({
      baseUrl: 'http://localhost:8090',
      fetch: fetch as unknown as typeof globalThis.fetch,
      timeoutMs: 0,
    })).toThrow(/positive integer/);
  });

  it('a delayed failure from a stale generation does not cancel the refreshed connection', async () => {
    let callCount = 0;
    let rejectGen1Fetch: ((error: Error) => void) | undefined;
    const harness2 = new StreamHarness();

    fetch.mockImplementation(() => {
      callCount += 1;
      if (callCount === 1) {
        return new Promise<Response>((_, reject) => { rejectGen1Fetch = reject; });
      }
      return new Response(harness2.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
    });

    const updates: QueryResult<unknown>[] = [];
    const transport = makeTransport(() => 'token', { maxReconnects: 5, timeoutMs: 5000 });
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (r) => updates.push(r),
    } as WatchOptions<unknown>);

    await wait(20);
    expect(fetch).toHaveBeenCalledTimes(1);

    // Refresh aborts gen1 (whose fetch is still hung) and opens gen2 on harness2.
    transport.refreshAuth();
    await wait(30);
    expect(fetch).toHaveBeenCalledTimes(2);

    // Confirm gen2 is live on harness2 before triggering the stale failure.
    harness2.push(sseEnvelope(encodeValue({ value: 'gen2-ok' }), id));
    await wait(10);
    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 'gen2-ok' }))).toBe(true);

    // Deliver the delayed failure from gen1's hung fetch. A stale generation must
    // only clean up its own resources and never touch this.reader (gen2's reader).
    rejectGen1Fetch!(new Error('gen1 socket reset'));
    await wait(30);

    expect(transport.connectionState).toBe('connected');
    expect(fetch).toHaveBeenCalledTimes(2);
    harness2.push(sseEnvelope(encodeValue({ value: 'survived' }), id));
    await wait(10);
    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 'survived' }))).toBe(true);
  });

  it('applies a server-negotiated subscribe maxEventSize bounded by the client ceiling', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const errors: Error[] = [];
    const updates: QueryResult<unknown>[] = [];
    const transport = makeTransport(undefined, { limits: { maxReturnValueBytes: 10000 } });
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: (r) => updates.push(r),
      onError: (e) => errors.push(e),
    } as WatchOptions<unknown>);

    await wait(10);
    // Server negotiates maxEventSize = 200 (legal: positive, below the client ceiling).
    harness.push(`data: ${JSON.stringify({ data: { id, op: 'subscribe', maxEventSize: 200 } })}\n\n`);
    await wait(10);
    // An event under the client ceiling (~14096) but over the negotiated 200 bytes.
    harness.push(sseEnvelope(encodeValue({ value: 'a'.repeat(300) }), id));
    await wait(10);
    // A small event within the negotiated limit still works.
    harness.push(sseEnvelope(encodeValue({ value: 'ok' }), id));
    await wait(10);

    expect(errors.some((e) => e.message.includes('maximum length'))).toBe(true);
    expect(updates.some((u) => JSON.stringify(u.data) === JSON.stringify({ value: 'ok' }))).toBe(true);
  });

  it('does not loosen the client ceiling when the server advertises a larger maxEventSize', async () => {
    const harness = new StreamHarness();
    fetch.mockImplementation(() => new Response(harness.stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }));

    const errors: Error[] = [];
    const transport = makeTransport(undefined, { limits: { maxReturnValueBytes: 64 } });
    const id = await subscriptionId('messages:list', { userId: 'u1' });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (e) => errors.push(e),
    } as WatchOptions<unknown>);

    await wait(10);
    // Server advertises a very large maxEventSize; the client must cap it.
    harness.push(`data: ${JSON.stringify({ data: { id, op: 'subscribe', maxEventSize: 999999999 } })}\n\n`);
    await wait(10);
    // An event over the client ceiling (~4160) must still be rejected.
    harness.push(sseEnvelope(encodeValue({ value: 'a'.repeat(5000) }), id));
    await wait(10);

    expect(errors.some((e) => e.message.includes('maximum length'))).toBe(true);
  });

  it('times out a fetch that hangs before response headers', async () => {
    let capturedSignal: AbortSignal | null | undefined;
    fetch.mockImplementation((_input: RequestInfo | URL, init?: RequestInit) => {
      capturedSignal = init?.signal;
      return new Promise<Response>((_, reject) => {
        if (init?.signal) {
          init.signal.addEventListener('abort', () => reject(new Error('AbortError')));
        }
      });
    });

    const errors: Error[] = [];
    const transport = makeTransport(undefined, { maxReconnects: 0, timeoutMs: 40 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (e) => errors.push(e),
    } as WatchOptions<unknown>);

    await wait(150);

    expect(errors.some((e) => /timeout/i.test(e.message))).toBe(true);
    expect(capturedSignal?.aborted).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
  });

  it('does not reset reconnect attempts on an undecodable message before EOF', async () => {
    const id = await subscriptionId('messages:list', { userId: 'u1' });
    const encoder = new TextEncoder();
    // A payload that passes envelope validation but fails decodeValue.
    const invalidEvent = `data: ${JSON.stringify({ data: { id, op: 'message', payload: { $bytes: 123 } } })}\n\n`;
    let callCount = 0;
    fetch.mockImplementation(() => {
      callCount += 1;
      const stream = new ReadableStream({
        start(controller) {
          controller.enqueue(encoder.encode(invalidEvent));
          controller.close();
        },
      });
      return new Response(stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
    });

    const errors: Error[] = [];
    const transport = makeTransport(undefined, { maxReconnects: 1, initialReconnectDelayMs: 5 });

    transport.watch('messages:list', { userId: 'u1' }, {
      onUpdate: () => {},
      onError: (e) => errors.push(e),
    } as WatchOptions<unknown>);

    await wait(200);

    // The undecodable message must not reset the counter, so EOF exhausts maxReconnects.
    expect(errors.some((e) => /malformed \$bytes/i.test(e.message))).toBe(true);
    expect(errors.some((e) => e.message.includes('reconnect limit'))).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('cleans up the establish timer on stale early returns (auth/hash/fetch/close)', async () => {
    vi.useFakeTimers();
    try {
      // --- refresh during auth ---
      {
        const hungAuth = vi.fn(() => new Promise<string>(() => {}));
        const hungFetch = vi.fn(() => new Promise<Response>(() => {}));
        const transport = new FetchRealtimeTransport({
          baseUrl: 'http://localhost:8090',
          fetch: hungFetch as unknown as typeof globalThis.fetch,
          getAuthToken: hungAuth,
          timeoutMs: 1000,
          maxReconnects: 5,
        });
        transports.push(transport);
        transport.watch('p', {}, { onUpdate: () => {} } as WatchOptions<unknown>);
        await vi.advanceTimersByTimeAsync(0);
        expect(vi.getTimerCount()).toBe(1);

        transport.refreshAuth();
        await vi.advanceTimersByTimeAsync(0);
        // gen1 returned early at the post-auth stale check; its establish timer
        // must be cleaned up. Only gen2's establish timer should remain.
        expect(vi.getTimerCount()).toBe(1);

        transport.close();
        await vi.advanceTimersByTimeAsync(0);
        expect(vi.getTimerCount()).toBe(0);
      }
      vi.clearAllTimers();

      // --- close during auth ---
      {
        const hungAuth = vi.fn(() => new Promise<string>(() => {}));
        const hungFetch = vi.fn(() => new Promise<Response>(() => {}));
        const transport = new FetchRealtimeTransport({
          baseUrl: 'http://localhost:8090',
          fetch: hungFetch as unknown as typeof globalThis.fetch,
          getAuthToken: hungAuth,
          timeoutMs: 1000,
        });
        transports.push(transport);
        transport.watch('p', {}, { onUpdate: () => {} } as WatchOptions<unknown>);
        await vi.advanceTimersByTimeAsync(0);
        expect(vi.getTimerCount()).toBe(1);

        transport.close();
        await vi.advanceTimersByTimeAsync(0);
        // gen1 returned early (isDisposed); no gen2. All timers cleared.
        expect(vi.getTimerCount()).toBe(0);
      }
      vi.clearAllTimers();

      // --- refresh during hash (computeSubscriptionId) ---
      {
        const hungFetch = vi.fn(() => new Promise<Response>(() => {}));
        const transport = new FetchRealtimeTransport({
          baseUrl: 'http://localhost:8090',
          fetch: hungFetch as unknown as typeof globalThis.fetch,
          timeoutMs: 1000,
          maxReconnects: 5,
        });
        transports.push(transport);
        const hashResolvers: ((id: string) => void)[] = [];
        transport.computeSubscriptionId = vi.fn(() => new Promise<string>((resolve) => {
          hashResolvers.push(resolve);
        })) as typeof transport.computeSubscriptionId;

        transport.watch('p', {}, { onUpdate: () => {} } as WatchOptions<unknown>);
        await vi.advanceTimersByTimeAsync(0);
        expect(vi.getTimerCount()).toBe(1);

        transport.refreshAuth();
        await vi.advanceTimersByTimeAsync(0);
        // gen1 is stuck at hash; gen2 has started. Resolve gen1's hash only.
        expect(hashResolvers.length).toBeGreaterThanOrEqual(1);
        hashResolvers[0]!('stale-id');
        await vi.advanceTimersByTimeAsync(0);
        // gen1 returned early at the post-hash stale check; timer cleaned up.
        expect(vi.getTimerCount()).toBe(1);
      }
      vi.clearAllTimers();

      // --- refresh during fetch (post-response stale check) ---
      {
        let callCount = 0;
        let resolveGen1Fetch: ((r: Response) => void) | undefined;
        const controllableFetch = vi.fn((): Promise<Response> => {
          callCount += 1;
          if (callCount === 1) {
            return new Promise<Response>((resolve) => { resolveGen1Fetch = resolve; });
          }
          return new Promise<Response>(() => {});
        });
        const transport = new FetchRealtimeTransport({
          baseUrl: 'http://localhost:8090',
          fetch: controllableFetch as unknown as typeof globalThis.fetch,
          timeoutMs: 1000,
          maxReconnects: 5,
        });
        transports.push(transport);
        // Bypass crypto.subtle.digest which does not resolve under fake timers.
        transport.computeSubscriptionId = vi.fn(() => Promise.resolve('fake-id')) as typeof transport.computeSubscriptionId;

        transport.watch('p', {}, { onUpdate: () => {} } as WatchOptions<unknown>);
        await vi.advanceTimersByTimeAsync(1);
        expect(vi.getTimerCount()).toBe(1);

        transport.refreshAuth();
        await vi.advanceTimersByTimeAsync(1);
        resolveGen1Fetch!(new Response('x', { status: 200 }));
        await vi.advanceTimersByTimeAsync(0);
        // gen1 returned early at the post-fetch stale check; timer cleaned up.
        expect(vi.getTimerCount()).toBe(1);
      }
    } finally {
      vi.useRealTimers();
    }
  });
});
