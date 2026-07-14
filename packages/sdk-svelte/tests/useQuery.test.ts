import { describe, it, expect, beforeEach } from 'vitest';
import { writable, get } from 'svelte/store';
import { useQuery, useQueryResult, useSubscription, createQuery, skip } from '../src/useQuery.js';
import { MockClient, addRef, doubleRef, emptyRef } from './mockClient.js';
import type { FunctionReference, QueryResult } from '@pbvex/sdk-core';

const intRef = {
  _path: 'math:int',
  _type: 'query',
} as FunctionReference<'query', { id: bigint }, string, 'public'>;

const bufferRef = {
  _path: 'math:buffer',
  _type: 'query',
} as FunctionReference<'query', { buf: ArrayBuffer }, string, 'public'>;

const anyRef = {
  _path: 'util:any',
  _type: 'query',
} as FunctionReference<'query', any, string, 'public'>;

function bufferFromBytes(bytes: number[]): ArrayBuffer {
  return new Uint8Array(bytes).buffer;
}

describe('useQuery', () => {
  let client: MockClient;

  beforeEach(() => {
    client = new MockClient();
  });

  it('emits undefined while loading and then the value when updated', () => {
    const store = useQuery(addRef, { a: 1, b: 2 }, client);
    const values: ({ sum: number } | undefined)[] = [];
    const unsubscribe = store.subscribe((v) => values.push(v));

    expect(values).toEqual([undefined]);
    expect(client.watchCalls.length).toBe(1);

    client.push({ data: { sum: 3 }, error: null, isLoading: false }, 'math:add', { a: 1, b: 2 });
    expect(values).toEqual([undefined, { sum: 3 }]);

    unsubscribe();
  });

  it('emits undefined and surfaces errors via useQueryResult', () => {
    const result = useQueryResult(addRef, { a: 1, b: 2 }, client);
    const results: QueryResult<{ sum: number }>[] = [];
    const unsubscribe = result.subscribe((r) => results.push(r));

    expect(results[0]).toEqual({ data: undefined, error: null, isLoading: true });

    const error = new Error('boom');
    client.pushError(error, 'math:add', { a: 1, b: 2 });

    expect(results[results.length - 1]).toEqual({ data: undefined, error, isLoading: false });

    const value = get(useQuery(addRef, { a: 1, b: 2 }, client));
    expect(value).toBeUndefined();

    unsubscribe();
  });

  it('supports skipping with the skip string', () => {
    const result = useQueryResult(addRef, 'skip', client);
    const results: QueryResult<{ sum: number }>[] = [];
    const unsubscribe = result.subscribe((r) => results.push(r));

    expect(results[results.length - 1]).toEqual({ data: undefined, error: null, isLoading: false });
    expect(client.watchCalls.length).toBe(0);

    const value = get(useQuery(addRef, 'skip', client));
    expect(value).toBeUndefined();

    unsubscribe();
  });

  it('supports skipping via a readable store', () => {
    const args = writable<typeof skip | { a: number; b: number }>('skip');
    const result = useQueryResult(addRef, args, client);
    const results: QueryResult<{ sum: number }>[] = [];
    const unsubscribe = result.subscribe((r) => results.push(r));

    expect(results[results.length - 1]).toEqual({ data: undefined, error: null, isLoading: false });
    expect(client.watchCalls.length).toBe(0);

    args.set({ a: 2, b: 3 });
    expect(client.watchCalls.length).toBe(1);
    client.push({ data: { sum: 5 }, error: null, isLoading: false }, 'math:add', { a: 2, b: 3 });
    expect(results[results.length - 1]).toEqual({ data: { sum: 5 }, error: null, isLoading: false });

    args.set('skip');
    expect(results[results.length - 1]).toEqual({ data: undefined, error: null, isLoading: false });

    unsubscribe();
  });

  it('dedupes reactive args by PBVex wire identity', () => {
    const args = writable<{ a: number; b: number }>({ a: 1, b: 2 });
    const result = useQueryResult(addRef, args, client);
    const results: QueryResult<{ sum: number }>[] = [];
    const unsubscribe = result.subscribe((r) => results.push(r));

    expect(client.watchCalls.length).toBe(1);

    args.set({ b: 2, a: 1 });
    expect(client.watchCalls.length).toBe(1);
    expect(results[results.length - 1]).toEqual({ data: undefined, error: null, isLoading: true });

    client.push({ data: { sum: 3 }, error: null, isLoading: false }, 'math:add', { a: 1, b: 2 });
    expect(results[results.length - 1]).toEqual({ data: { sum: 3 }, error: null, isLoading: false });

    args.set({ a: 2, b: 3 });
    expect(client.watchCalls.length).toBe(2);
    client.push({ data: { sum: 5 }, error: null, isLoading: false }, 'math:add', { a: 2, b: 3 });
    expect(results[results.length - 1]).toEqual({ data: { sum: 5 }, error: null, isLoading: false });

    unsubscribe();
  });

  it('distinguishes omitted args from explicit null in reactive stores', () => {
    const args = writable<any>(undefined);
    const result = useQueryResult(anyRef, args, client);
    const unsubscribe = result.subscribe(() => {});

    expect(client.watchCalls).toHaveLength(1);
    expect(client.watchCalls[0]?.args).toBeUndefined();

    args.set(null);
    expect(client.watchCalls).toHaveLength(2);
    expect(client.watchCalls[1]?.args).toBeNull();

    unsubscribe();
  });

  it('dedupes BigInt and ArrayBuffer wire identity', () => {
    const intArgs = writable<{ id: bigint }>({ id: 1n });
    const intResult = useQueryResult(intRef, intArgs, client);
    const intResults: QueryResult<string>[] = [];
    const unsubscribeInt = intResult.subscribe((r) => intResults.push(r));

    expect(client.watchCalls.length).toBe(1);
    intArgs.set({ id: 1n });
    expect(client.watchCalls.length).toBe(1);

    client.push({ data: 'one', error: null, isLoading: false }, 'math:int', { id: 1n });
    expect(intResults[intResults.length - 1]).toEqual({ data: 'one', error: null, isLoading: false });

    unsubscribeInt();

    const bufArgs = writable<{ buf: ArrayBuffer }>({ buf: bufferFromBytes([1, 2, 3]) });
    const bufResult = useQueryResult(bufferRef, bufArgs, client);
    const bufResults: QueryResult<string>[] = [];
    const unsubscribeBuf = bufResult.subscribe((r) => bufResults.push(r));

    expect(client.watchCalls.length).toBe(2);
    bufArgs.set({ buf: bufferFromBytes([1, 2, 3]) });
    expect(client.watchCalls.length).toBe(2);

    client.push({ data: 'ok', error: null, isLoading: false }, 'math:buffer', { buf: bufferFromBytes([1, 2, 3]) });
    expect(bufResults[bufResults.length - 1]).toEqual({ data: 'ok', error: null, isLoading: false });

    unsubscribeBuf();
  });

  it('switches atomically when reactive args change and ignores stale updates', () => {
    const args = writable<{ n: number }>({ n: 1 });
    const result = useQueryResult(doubleRef, args, client);
    const results: QueryResult<number>[] = [];
    const unsubscribe = result.subscribe((r) => results.push(r));

    expect(results[results.length - 1]).toEqual({ data: undefined, error: null, isLoading: true });
    expect(client.watchCalls[0].args).toEqual({ n: 1 });

    const firstCall = client.watchCalls[0];

    args.set({ n: 2 });
    firstCall.options.onUpdate({ data: 999, error: null, isLoading: false });

    expect(results[results.length - 1].data).not.toBe(999);
    expect(client.watchCalls[client.watchCalls.length - 1].args).toEqual({ n: 2 });

    client.push({ data: 4, error: null, isLoading: false }, 'math:double', { n: 2 });
    expect(results[results.length - 1]).toEqual({ data: 4, error: null, isLoading: false });

    unsubscribe();
  });

  it('shares one watch across multiple subscribers of the same store', () => {
    const store = useQueryResult(addRef, { a: 1, b: 2 }, client);
    const resultsA: QueryResult<{ sum: number }>[] = [];
    const resultsB: QueryResult<{ sum: number }>[] = [];

    const unsubscribeA = store.subscribe((r) => resultsA.push(r));
    const unsubscribeB = store.subscribe((r) => resultsB.push(r));

    expect(client.watchCalls.length).toBe(1);

    client.push({ data: { sum: 7 }, error: null, isLoading: false }, 'math:add', { a: 1, b: 2 });

    expect(resultsA[resultsA.length - 1]).toEqual({ data: { sum: 7 }, error: null, isLoading: false });
    expect(resultsB[resultsB.length - 1]).toEqual({ data: { sum: 7 }, error: null, isLoading: false });

    unsubscribeA();
    expect(client.subscriptions.size).toBe(1);

    unsubscribeB();
    expect(client.subscriptions.size).toBe(0);
  });

  it('teardowns and restarts subscriptions when the last subscriber leaves and rejoins', () => {
    const store = useQueryResult(addRef, { a: 1, b: 2 }, client);
    const unsubscribe = store.subscribe(() => {});

    expect(client.watchCalls.length).toBe(1);
    unsubscribe();

    expect(client.subscriptions.size).toBe(0);

    const unsubscribe2 = store.subscribe(() => {});
    expect(client.watchCalls.length).toBe(2);
    unsubscribe2();
  });

  it('useSubscription is a compatible alias for useQueryResult', () => {
    const result = useSubscription(addRef, { a: 1, b: 2 }, client);
    const results: QueryResult<{ sum: number }>[] = [];
    const unsubscribe = result.subscribe((r) => results.push(r));

    client.push({ data: { sum: 9 }, error: null, isLoading: false }, 'math:add', { a: 1, b: 2 });
    expect(results[results.length - 1]).toEqual({ data: { sum: 9 }, error: null, isLoading: false });

    unsubscribe();
  });

  it('createQuery is an alias for useQuery', () => {
    const store = createQuery(addRef, { a: 1, b: 2 }, client);
    const values: ({ sum: number } | undefined)[] = [];
    const unsubscribe = store.subscribe((v) => values.push(v));

    client.push({ data: { sum: 11 }, error: null, isLoading: false }, 'math:add', { a: 1, b: 2 });
    expect(values[values.length - 1]).toEqual({ sum: 11 });

    unsubscribe();
  });

  it('supports empty-args queries with and without explicit client', () => {
    const result = useQueryResult(emptyRef, client);
    const results: QueryResult<string>[] = [];
    const unsubscribe = result.subscribe((r) => results.push(r));

    expect(client.watchCalls.length).toBe(1);
    expect(client.watchCalls[0].args).toBeUndefined();

    client.push({ data: 'hello', error: null, isLoading: false }, 'util:empty');
    expect(results[results.length - 1]).toEqual({ data: 'hello', error: null, isLoading: false });

    unsubscribe();
  });

  it('ignores stale callbacks when switching active args to skip', () => {
    const args = writable<{ a: number; b: number } | 'skip'>({ a: 1, b: 2 });
    const result = useQueryResult(addRef, args, client);
    const results: QueryResult<{ sum: number }>[] = [];
    const unsubscribe = result.subscribe((r) => results.push(r));

    const firstCall = client.watchCalls[0];
    client.push({ data: { sum: 3 }, error: null, isLoading: false }, 'math:add', { a: 1, b: 2 });

    args.set('skip');
    expect(results[results.length - 1]).toEqual({ data: undefined, error: null, isLoading: false });

    firstCall.options.onUpdate({ data: { sum: 999 }, error: null, isLoading: false });
    firstCall.options.onError?.(new Error('stale error'));

    expect(results[results.length - 1]).toEqual({ data: undefined, error: null, isLoading: false });

    unsubscribe();
  });

  it('ignores stale callbacks after last subscriber leaves and rejoins', () => {
    const args = writable<{ a: number; b: number }>({ a: 1, b: 2 });
    const result = useQueryResult(addRef, args, client);
    const unsubscribe = result.subscribe(() => {});

    const firstCall = client.watchCalls[0];
    unsubscribe();

    firstCall.options.onUpdate({ data: { sum: 999 }, error: null, isLoading: false });

    const results: QueryResult<{ sum: number }>[] = [];
    const unsubscribe2 = result.subscribe((r) => results.push(r));

    expect(results[0]).toEqual({ data: undefined, error: null, isLoading: true });

    unsubscribe2();
  });
});
