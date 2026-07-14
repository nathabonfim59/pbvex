import { describe, it, expect, vi } from 'vitest';
import { Client, PBVexClient } from '../src/index.js';
import type { FunctionReference, ArgsOf, ReturnOf } from '../src/index.js';
import { api, internal } from './fixtures/api.js';

const addRef = {
  _path: 'math:add',
  _type: 'query',
  _visibility: 'public',
} as FunctionReference<'query', { a: number; b: number }, { sum: number }, 'public'>;

const emptyRef = {
  _path: 'util:empty',
  _type: 'query',
  _visibility: 'public',
  __noArgs: true,
} as FunctionReference<'query', void, string, 'public'>;

const anyRef = {
  _path: 'util:any',
  _type: 'query',
  _visibility: 'public',
} as FunctionReference<'query', any, string, 'public'>;

const unknownRef = {
  _path: 'util:unknown',
  _type: 'query',
  _visibility: 'public',
} as FunctionReference<'query', unknown, string, 'public'>;

const nullRef = {
  _path: 'util:null',
  _type: 'query',
  _visibility: 'public',
} as FunctionReference<'query', null, string, 'public'>;

const allOptionalRef = {
  _path: 'util:allOptional',
  _type: 'query',
  _visibility: 'public',
} as FunctionReference<'query', { filter?: string }, string, 'public'>;

const neverRef = {
  _path: 'util:never',
  _type: 'query',
  _visibility: 'public',
  __noArgs: true,
} as FunctionReference<'query', never, string, 'public'>;

const mutRef = {
  _path: 'math:mut',
  _type: 'mutation',
  _visibility: 'public',
} as FunctionReference<'mutation', { value: number }, boolean, 'public'>;

const internalRef = {
  _path: 'internal:secret',
  _type: 'query',
  _visibility: 'internal',
} as FunctionReference<'query', { key: string }, string, 'internal'>;

function createClient(): Client {
  const fetch = vi.fn(() => new Promise<Response>(() => {})) as unknown as typeof globalThis.fetch;
  return new Client('http://localhost:8090', { fetch });
}

describe('type inference', () => {
  it('infers args and return from a FunctionReference', () => {
    type Args = ArgsOf<typeof addRef>;
    type Return = ReturnOf<typeof addRef>;

    const ok: Args = { a: 1, b: 2 };
    const result: Return = { sum: 3 };

    expect(ok).toBeDefined();
    expect(result).toBeDefined();
  });

  it('required args must be supplied', () => {
    const client = createClient();

    // @ts-expect-error missing required args
    void client.query(addRef);

    // @ts-expect-error wrong argument type
    void client.query(addRef, { a: 1, b: '2' });

    // @ts-expect-error missing a required field
    void client.query(addRef, { a: 1 });

    expect(client).toBeInstanceOf(Client);
  });

  it('empty/void args can be omitted', () => {
    const client = createClient();

    const p1 = client.query(emptyRef);
    const p2 = client.query(emptyRef, undefined);
    const p3 = client.mutation('util:empty');

    expect(p1).toBeInstanceOf(Promise);
    expect(p2).toBeInstanceOf(Promise);
    expect(p3).toBeInstanceOf(Promise);
  });

  it('distinguishes any, unknown, null, all-optional, empty, void, and never', () => {
    const client = createClient();

    // any: caller may omit the untyped argument
    void client.query(anyRef, {});
    void client.query(anyRef);

    // unknown: the shared authoring contract permits omission
    void client.query(unknownRef, 'x');
    void client.query(unknownRef);

    // null: required arg
    void client.query(nullRef, null);
    // @ts-expect-error null is not optional
    void client.query(nullRef);
    // @ts-expect-error undefined is not null
    void client.query(nullRef, undefined);

    // all-optional: may be omitted
    void client.query(allOptionalRef, {});
    void client.query(allOptionalRef, { filter: 'x' });
    void client.query(allOptionalRef);
    // @ts-expect-error wrong field type
    void client.query(allOptionalRef, { filter: 1 });

    // void: optional
    void client.query(emptyRef);
    void client.query(emptyRef, undefined);
    // The sole slot is CallOptions for a no-args reference.
    void client.query(emptyRef, {});

    // never: optional
    void client.query(neverRef);
    void client.query(neverRef, undefined);
    // @ts-expect-error string is not never
    void client.query(neverRef, 'x');

    // wrong kind
    // @ts-expect-error query expects a query ref
    void client.query(mutRef);
    // @ts-expect-error mutation expects a mutation ref
    void client.mutation(addRef);
    // @ts-expect-error action expects an action ref
    void client.action(mutRef);

    expect(client).toBeInstanceOf(Client);
  });

  it('rejects internal references from public client methods', () => {
    const client = createClient();

    // Supply otherwise-valid args/options so the only failing constraint is
    // visibility; this catches a regression that silently accepts internal refs.
    // @ts-expect-error internal refs are not public
    void client.query(internalRef, { key: 'x' });
    // @ts-expect-error internal refs are not public
    void client.watch(internalRef, { key: 'x' }, { onUpdate: () => {} });
    // @ts-expect-error internal refs are not public
    void client.query(internal.messages.admin, { secret: 'x' });
    // @ts-expect-error internal refs are not public even with options
    void client.query(internal.messages.admin, { secret: 'x' }, { timeoutMs: 1000 });

    expect(client).toBeInstanceOf(Client);
  });

  it('PBVexClient is a Client alias', () => {
    const client = new PBVexClient('http://localhost:8090');
    expect(client).toBeInstanceOf(Client);
  });

  it('accepts a pbvex-generated API reference', () => {
    const client = createClient();

    void client.query(api.messages.list, { channel: 'general' });
    void client.mutation(api.messages.send, { body: 'hello' });

    // @ts-expect-error missing required args
    void client.query(api.messages.list);

    // @ts-expect-error wrong args type
    void client.mutation(api.messages.send, { body: 123 });

    expect(client).toBeInstanceOf(Client);
  });
});
