import { describe, it, expect, vi, afterEach } from 'vitest';
import { FetchRealtimeTransport } from '../src/realtime.js';
import { PBVexError } from '../src/errors.js';
import { encodeValue, canonicalJson, hashSha256 } from '@pbvex/protocol';
import type { ConnectionState, QueryResult, WatchOptions } from '../src/index.js';

const SESSION_ID = 'a'.repeat(64);
const SESSION_URL = 'http://localhost:8090/api/pbvex/realtime/session';
const CONTROL_URL = 'http://localhost:8090/api/pbvex/realtime/session/subscriptions';

async function subscriptionId(path: string, args: unknown): Promise<string> {
  return hashSha256(`v1:${path}:${canonicalJson(encodeValue(args as any))}`);
}

function event(data: Record<string, unknown>): string {
  return `data: ${JSON.stringify({ data })}\n\n`;
}

interface ControlBody {
  session: string;
  subscribe: { id: string; path: string; args: unknown }[];
  unsubscribe: string[];
}

/** A fake server for the session protocol. */
class FakeServer {
  streams: { controller: ReadableStreamDefaultController<Uint8Array>; signal: AbortSignal; headers: Record<string, string> }[] = [];
  controls: ControlBody[] = [];
  controlStatus = 204;
  sessionStatus = 200;
  private encoder = new TextEncoder();

  readonly fetch = vi.fn(async (url: string, init: RequestInit): Promise<Response> => {
    if (url === SESSION_URL) {
      if (this.sessionStatus !== 200) {
        return new Response(JSON.stringify({ status: this.sessionStatus, message: 'Not found.' }), { status: this.sessionStatus });
      }
      const signal = init.signal!;
      const stream = new ReadableStream<Uint8Array>({
        start: (controller) => {
          this.streams.push({ controller, signal, headers: init.headers as Record<string, string> });
          signal.addEventListener('abort', () => {
            try {
              controller.error(new DOMException('Aborted', 'AbortError'));
            } catch {
              // already closed
            }
          });
        },
      });
      return new Response(stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
    }
    if (url === CONTROL_URL) {
      this.controls.push(JSON.parse(init.body as string));
      return new Response(null, { status: this.controlStatus });
    }
    throw new Error(`unexpected url ${url}`);
  });

  get current() {
    return this.streams[this.streams.length - 1]!;
  }

  push(data: Record<string, unknown>): void {
    this.current.controller.enqueue(this.encoder.encode(event(data)));
  }

  announce(id = SESSION_ID): void {
    this.push({ id, op: 'session' });
  }

  message(id: string, value: unknown): void {
    this.push({ id, op: 'message', payload: encodeValue(value as any) });
  }

  end(): void {
    this.current.controller.close();
  }

  subscribedIds(): string[] {
    return this.controls.flatMap((c) => c.subscribe.map((s) => s.id));
  }
}

function wait(ms = 10): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

describe('FetchRealtimeTransport sessions', () => {
  const transports: FetchRealtimeTransport[] = [];
  afterEach(() => {
    transports.splice(0).forEach((t) => t.close());
  });

  function makeTransport(server: FakeServer, opts: Partial<ConstructorParameters<typeof FetchRealtimeTransport>[0]> = {}) {
    const transport = new FetchRealtimeTransport({
      baseUrl: 'http://localhost:8090',
      fetch: server.fetch as unknown as typeof globalThis.fetch,
      initialReconnectDelayMs: 5,
      ...opts,
    });
    transports.push(transport);
    return transport;
  }

  it('carries many distinct subscriptions over a single stream', async () => {
    const server = new FakeServer();
    const transport = makeTransport(server);
    const updates = new Map<number, QueryResult<unknown>[]>();
    for (let take = 1; take <= 8; take++) {
      updates.set(take, []);
      transport.watch('items:list', { take }, { onUpdate: (r) => updates.get(take)!.push(r) } as WatchOptions<unknown>);
    }
    await wait();
    server.announce();
    await wait();

    expect(server.streams).toHaveLength(1);
    const ids = await Promise.all([1, 2, 3, 4, 5, 6, 7, 8].map((take) => subscriptionId('items:list', { take })));
    expect(server.subscribedIds().sort()).toEqual([...ids].sort());
    expect(server.controls.every((c) => c.session === SESSION_ID)).toBe(true);
    expect(server.fetch.mock.calls.filter(([url]) => url === CONTROL_URL).length).toBeLessThanOrEqual(2);

    server.message(ids[2]!, { take: 3 });
    server.message(ids[6]!, { take: 7 });
    await wait();
    expect(updates.get(3)!.at(-1)).toEqual({ data: { take: 3 }, error: null, isLoading: false });
    expect(updates.get(7)!.at(-1)).toEqual({ data: { take: 7 }, error: null, isLoading: false });
    expect(updates.get(1)!.at(-1)?.isLoading).toBe(true);
    expect(transport.connectionState).toBe('connected');
  });

  it('deduplicates identical watches and unsubscribes once the last watcher leaves', async () => {
    const server = new FakeServer();
    const transport = makeTransport(server);
    const a = transport.watch('q', { x: 1 }, { onUpdate: () => {} } as WatchOptions<unknown>);
    const b = transport.watch('q', { x: 1 }, { onUpdate: () => {} } as WatchOptions<unknown>);
    const other = transport.watch('q', { x: 2 }, { onUpdate: () => {} } as WatchOptions<unknown>);
    await wait();
    server.announce();
    await wait();
    expect(server.subscribedIds()).toHaveLength(2);

    const id1 = await subscriptionId('q', { x: 1 });
    a();
    await wait();
    expect(server.controls.flatMap((c) => c.unsubscribe)).toEqual([]);
    b();
    await wait();
    expect(server.controls.flatMap((c) => c.unsubscribe)).toEqual([id1]);
    expect(server.current.signal.aborted).toBe(false);

    // Without subscriptions the stream is released.
    other();
    await wait();
    expect(server.current.signal.aborted).toBe(true);
    expect(transport.connectionState).toBe('disconnected');
  });

  it('routes a structured error to its own subscription only', async () => {
    const server = new FakeServer();
    const transport = makeTransport(server);
    const failing: QueryResult<unknown>[] = [];
    const errors: Error[] = [];
    const healthy: QueryResult<unknown>[] = [];
    transport.watch('missing', {}, { onUpdate: (r) => failing.push(r), onError: (e) => errors.push(e) } as WatchOptions<unknown>);
    transport.watch('ok', {}, { onUpdate: (r) => healthy.push(r) } as WatchOptions<unknown>);
    await wait();
    server.announce();
    await wait();

    server.message(await subscriptionId('missing', {}), { error: true, code: 'not_found', message: 'Function not found.', details: [] });
    server.message(await subscriptionId('ok', {}), 'fine');
    await wait();
    expect(failing.at(-1)?.error).toBeInstanceOf(PBVexError);
    expect(errors[0]).toBeInstanceOf(PBVexError);
    expect(healthy.at(-1)).toEqual({ data: 'fine', error: null, isLoading: false });
  });

  it('reopens the session and resubscribes everything when the stream ends', async () => {
    const server = new FakeServer();
    const transport = makeTransport(server);
    const states: ConnectionState[] = [];
    transport.watch('a', {}, { onUpdate: () => {}, onConnectionStateChange: (s) => states.push(s) } as WatchOptions<unknown>);
    transport.watch('b', {}, { onUpdate: () => {} } as WatchOptions<unknown>);
    await wait();
    server.announce();
    await wait();
    expect(server.subscribedIds()).toHaveLength(2);

    server.end();
    await wait(30);
    expect(server.streams).toHaveLength(2);
    expect(states).toContain('reconnecting');
    server.announce('b'.repeat(64));
    await wait();
    const resubscribe = server.controls.at(-1)!;
    expect(resubscribe.session).toBe('b'.repeat(64));
    expect(resubscribe.subscribe.map((s) => s.path).sort()).toEqual(['a', 'b']);
    expect(states.at(-1)).toBe('connected');
  });

  it('reopens the session when a control request finds it gone', async () => {
    const server = new FakeServer();
    server.controlStatus = 404;
    const transport = makeTransport(server);
    transport.watch('a', {}, { onUpdate: () => {} } as WatchOptions<unknown>);
    await wait();
    server.announce();
    await wait(30);
    expect(server.streams[0]!.signal.aborted).toBe(true);
    expect(server.streams).toHaveLength(2);
  });

  it('reopens the session with fresh credentials on refreshAuth', async () => {
    const server = new FakeServer();
    let token = 'one';
    const transport = makeTransport(server, { getAuthToken: () => token });
    transport.watch('a', {}, { onUpdate: () => {} } as WatchOptions<unknown>);
    await wait();
    server.announce();
    await wait();
    expect(server.current.headers.Authorization).toBe('Bearer one');

    token = 'two';
    transport.refreshAuth();
    await wait();
    expect(server.streams[0]!.signal.aborted).toBe(true);
    expect(server.current.headers.Authorization).toBe('Bearer two');
    server.announce('c'.repeat(64));
    await wait();
    expect(server.controls.at(-1)!.subscribe.map((s) => s.path)).toEqual(['a']);
  });

  it('explains a missing session endpoint', async () => {
    const server = new FakeServer();
    server.sessionStatus = 404;
    const transport = makeTransport(server, { maxReconnects: 0 });
    const errors: Error[] = [];
    transport.watch('a', {}, { onUpdate: () => {}, onError: (e) => errors.push(e) } as WatchOptions<unknown>);
    await wait(30);
    expect(errors[0]?.message).toMatch(/does not support realtime sessions/);
    expect(errors.at(-1)?.message).toMatch(/reconnect limit/);
    expect(transport.connectionState).toBe('disconnected');
  });
});
