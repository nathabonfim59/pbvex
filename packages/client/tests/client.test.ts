import { describe, it, expect, vi } from 'vitest';
import { Client, PBVexClient, PBVexError } from '../src/index.js';
import { encodeValue } from '@pbvex/protocol';
import type { FunctionReference } from '../src/index.js';
import type { MockedFunction } from 'vitest';

async function requestJson(request: Request): Promise<unknown> {
  return JSON.parse(await request.text());
}

function makeMockFetch(handler: (request: Request) => Promise<Response> | Response): MockedFunction<typeof fetch> {
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    const result = handler(request);
    return Promise.resolve(result);
  }) as unknown as MockedFunction<typeof fetch>;
}

function okResponse(body: { result: unknown }): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

const addRef = {
  _path: 'math:add',
  _type: 'query',
  _visibility: 'public',
} as FunctionReference<'query', { a: number; b: number }, number, 'public'>;

const noArgsRef = {
  _path: 'health:ping',
  _type: 'query',
  _visibility: 'public',
  __noArgs: true,
} as const satisfies FunctionReference<'query', void, boolean, 'public'>;

describe('Client', () => {
  it('sends a POST to /api/pbvex/call with encoded args', async () => {
    const fetch = makeMockFetch(async (request) => {
      expect(request.url).toBe('http://localhost:8090/api/pbvex/call');
      expect(request.method).toBe('POST');
      expect(request.headers.get('content-type')).toBe('application/json');
      const body = await requestJson(request) as { name: string; args: unknown };
      expect(body.name).toBe('math:add');
      expect(body.args).toEqual({ a: 1, b: 2 });
      return okResponse({ result: 42 });
    });

    const client = new Client('http://localhost:8090', { fetch });
    const result = await client.query(addRef, { a: 1, b: 2 });

    expect(result).toBe(42);
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('supports string overloads for query/mutation/action', async () => {
    const fetch = makeMockFetch(async (request) => {
      const body = await requestJson(request) as { name: string };
      return okResponse({ result: body.name });
    });

    const client = new Client('http://localhost:8090', { fetch });

    expect(await client.query('math:q', { x: 1 })).toBe('math:q');
    expect(await client.mutation('math:m', { x: 1 })).toBe('math:m');
    expect(await client.action('math:a', { x: 1 })).toBe('math:a');
  });

  it('roundtrips bigint and bytes', async () => {
    const fetch = makeMockFetch(async (request) => {
      const body = await requestJson(request) as { name: string; args: unknown };
      expect(body.args).toEqual(encodeValue({ id: 9223372036854775807n, blob: new Uint8Array([1, 2, 3]).buffer }));
      return okResponse({ result: { $bytes: 'Ag==' } });
    });

    const client = new Client('http://localhost:8090', { fetch });
    const result = await client.query('values', {
      id: 9223372036854775807n,
      blob: new Uint8Array([1, 2, 3]).buffer,
    });

    expect(result).toBeInstanceOf(ArrayBuffer);
    expect(new Uint8Array(result as ArrayBuffer)).toEqual(new Uint8Array([2]));
  });

  it('sets and clears Bearer auth', async () => {
    const fetch = makeMockFetch((request) => {
      expect(request.headers.get('authorization')).toBe('Bearer token-a');
      return okResponse({ result: null });
    });

    const client = new Client('http://localhost:8090', { fetch });
    client.setAuth('token-a');
    await client.query('foo');

    fetch.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const request = new Request(input, init);
      expect(request.headers.get('authorization')).toBeNull();
      return Promise.resolve(okResponse({ result: null }));
    });

    client.clearAuth();
    await client.query('foo');
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('uses a per-call auth provider', async () => {
    const fetch = makeMockFetch((request) => {
      expect(request.headers.get('authorization')).toBe('Bearer dynamic');
      return okResponse({ result: null });
    });

    const client = new Client('http://localhost:8090', {
      fetch,
      auth: () => 'dynamic',
    });
    await client.query('foo');
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('throws PBVexError on structured error responses', async () => {
    const fetch = makeMockFetch(() =>
      new Response(
        JSON.stringify({ error: true, code: 'unauthorized', message: 'nope' }),
        { status: 401, headers: { 'Content-Type': 'application/json' } },
      ),
    );

    const client = new Client('http://localhost:8090', { fetch });
    await expect(client.query('foo')).rejects.toBeInstanceOf(PBVexError);
    await expect(client.query('foo')).rejects.toThrow('nope');
    try {
      await client.query('foo');
    } catch (error) {
      expect(error).toBeInstanceOf(PBVexError);
      expect((error as PBVexError).name).toBe('PBVexError');
    }
  });

  it('preserves storage-specific structured error codes', async () => {
    const fetch = makeMockFetch(() =>
      new Response(
        JSON.stringify({ error: true, code: 'storage_full', message: 'capacity reached' }),
        { status: 507, headers: { 'Content-Type': 'application/json' } },
      ),
    );

    const client = new Client('http://localhost:8090', { fetch });
    try {
      await client.mutation('files:createUpload');
      throw new Error('expected mutation to reject');
    } catch (error) {
      expect(error).toBeInstanceOf(PBVexError);
      expect((error as PBVexError).code).toBe('storage_full');
      expect((error as PBVexError).message).toBe('capacity reached');
    }
  });

  it('decodes application error data into PBVex values', async () => {
    const fetch = makeMockFetch(() =>
      new Response(
        JSON.stringify({
          error: true,
          code: 'conflict',
          message: 'Conflict.',
          data: { resource: 'note', retryAfter: { $integer: 'AQAAAAAAAAA=' } },
        }),
        { status: 409, headers: { 'Content-Type': 'application/json' } },
      ),
    );

    const client = new Client('http://localhost:8090', { fetch });
    try {
      await client.mutation('notes:create');
      throw new Error('expected application error');
    } catch (error) {
      expect(error).toBeInstanceOf(PBVexError);
      expect((error as PBVexError).code).toBe('conflict');
      expect((error as PBVexError).data).toEqual({ resource: 'note', retryAfter: 1n });
    }
  });

  it('throws on malformed response without result', async () => {
    const fetch = makeMockFetch(() =>
      new Response(JSON.stringify({ unexpected: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );

    const client = new Client('http://localhost:8090', { fetch });
    await expect(client.query('foo')).rejects.toThrow('Malformed response: missing or invalid result');
  });

  it('times out long requests', async () => {
    const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const request = new Request(input, init);
      return new Promise((_resolve, reject) => {
        if (request.signal) {
          request.signal.addEventListener('abort', () => {
            reject(new Error('AbortError'));
          });
        }
      });
    }) as unknown as MockedFunction<typeof globalThis.fetch>;

    const client = new Client('http://localhost:8090', { fetch, timeoutMs: 50 });
    await expect(client.query('foo')).rejects.toThrow('timeout');
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('times out slow auth providers', async () => {
    const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const request = new Request(input, init);
      return new Promise((_resolve, reject) => {
        if (request.signal) {
          request.signal.addEventListener('abort', () => {
            reject(new Error('AbortError'));
          });
        }
      });
    }) as unknown as MockedFunction<typeof globalThis.fetch>;

    const client = new Client('http://localhost:8090', {
      fetch,
      timeoutMs: 50,
      auth: () => new Promise(() => {}),
    });
    await expect(client.query('foo')).rejects.toThrow('timeout');
    expect(fetch).toHaveBeenCalledTimes(0);
  });

  it('times out slow body reads', async () => {
    const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const request = new Request(input, init);
      const stream = new ReadableStream({
        start(controller) {
          if (request.signal) {
            request.signal.addEventListener('abort', () => {
              controller.close();
            });
          }
        },
      });
      return Promise.resolve(new Response(stream, { status: 200, headers: { 'Content-Type': 'application/json' } }));
    }) as unknown as MockedFunction<typeof globalThis.fetch>;

    const client = new Client('http://localhost:8090', { fetch, timeoutMs: 50 });
    await expect(client.query('foo')).rejects.toThrow('timeout');
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('aborts with an external signal', async () => {
    const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const request = new Request(input, init);
      return new Promise((_resolve, reject) => {
        if (request.signal) {
          request.signal.addEventListener('abort', () => {
            reject(new Error('AbortError'));
          });
        }
      });
    }) as unknown as MockedFunction<typeof globalThis.fetch>;

    const client = new Client('http://localhost:8090', { fetch });
    const controller = new AbortController();
    const promise = client.query('foo', undefined, { signal: controller.signal });
    controller.abort();
    await expect(promise).rejects.toThrow(/This operation was aborted|Aborted/);
  });

  it('uses __noArgs to distinguish a sole options slot from args', async () => {
    const seen: unknown[] = [];
    const fetch = makeMockFetch(async (request) => {
      seen.push((await requestJson(request) as { args: unknown }).args);
      return okResponse({ result: true });
    });
    const client = new Client('http://localhost:8090', { fetch });

    await client.query(noArgsRef, { timeoutMs: 1000 });
    expect(seen).toEqual([{}]);
  });

  it('does not mistake option-shaped required args for call options', async () => {
    const ref = {
      _path: 'settings:update',
      _type: 'mutation',
      _visibility: 'public',
    } as const satisfies FunctionReference<'mutation', { timeoutMs: number }, { timeoutMs: number }, 'public'>;
    const fetch = makeMockFetch(async (request) => {
      const args = (await requestJson(request) as { args: unknown }).args;
      return okResponse({ result: args });
    });
    const client = new Client('http://localhost:8090', { fetch });

    await expect(client.mutation(ref, { timeoutMs: 123 })).resolves.toEqual({ timeoutMs: 123 });
  });

  it('accepts the deprecated abort alias', async () => {
    const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const request = new Request(input, init);
      return new Promise((_resolve, reject) => {
        request.signal.addEventListener('abort', () => reject(new Error('AbortError')));
      });
    }) as unknown as MockedFunction<typeof globalThis.fetch>;
    const client = new Client('http://localhost:8090', { fetch });
    const controller = new AbortController();
    const promise = client.query('foo', undefined, { abort: controller.signal });
    controller.abort();

    await expect(promise).rejects.toThrow(/This operation was aborted|Aborted/);
  });

  it('uses PBVexClient alias', () => {
    const fetch = makeMockFetch(() => okResponse({ result: null }));
    const client = new PBVexClient('http://localhost:8090', { fetch });
    expect(client).toBeInstanceOf(Client);
  });

  it('enforces configured maxFunctionArgsBytes on call args', async () => {
    const fetch = makeMockFetch(() => okResponse({ result: null }));
    const client = new Client('http://localhost:8090', { fetch, limits: { maxFunctionArgsBytes: 64 } });
    await expect(client.query('foo', { data: 'x'.repeat(100) })).rejects.toThrow('Function args exceed');
    expect(fetch).toHaveBeenCalledTimes(0);
  });

  it('enforces configured maxReturnValueBytes on call response bodies', async () => {
    const big = 'x'.repeat(5000);
    const fetch = makeMockFetch(() => okResponse({ result: big }));
    const client = new Client('http://localhost:8090', { fetch, limits: { maxReturnValueBytes: 100 } });
    await expect(client.query('foo')).rejects.toThrow('exceeds');
  });

  it('validates configurable client limits', () => {
    const fetch = makeMockFetch(() => okResponse({ result: null }));
    expect(() => new Client('http://localhost:8090', { fetch, limits: { maxFunctionArgsBytes: -1 } })).toThrow(/non-negative integer/);
    expect(() => new Client('http://localhost:8090', { fetch, limits: { maxReturnValueBytes: 1.5 } })).toThrow(/non-negative integer/);
  });
});
