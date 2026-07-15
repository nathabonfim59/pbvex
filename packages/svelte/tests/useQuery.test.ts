import { describe, it, expect, beforeEach } from 'vitest';
import { flushSync, mount, tick, unmount } from 'svelte';
import QueryHarness from './QueryHarness.svelte';
import { MockClient, addRef, doubleRef } from './mockClient.js';
import type { QueryState } from '../src/useQuery.svelte.js';

type Harness = {
  query: QueryState<unknown>;
  setArgs(next: unknown): void;
};

function createHarness(
  client: MockClient,
  ref: import('@pbvex/client').FunctionReference<'query', any, any> = addRef,
  initialArgs: unknown = { a: 1, b: 2 },
) {
  return mount(QueryHarness, {
    target: document.body,
    props: { client, ref, initialArgs },
  }) as Harness;
}

describe('useQuery', () => {
  let client: MockClient;

  beforeEach(() => {
    client = new MockClient();
    document.body.replaceChildren();
  });

  it('exposes rune-backed loading, data, and error state', async () => {
    const harness = createHarness(client);
    await tick();

    expect(harness.query).toEqual({ data: undefined, error: null, isLoading: true });
    expect(client.watchCalls).toHaveLength(1);

    client.push({ data: { sum: 3 }, error: null, isLoading: false }, 'math:add', { a: 1, b: 2 });
    expect(harness.query).toEqual({ data: { sum: 3 }, error: null, isLoading: false });

    const error = new Error('boom');
    client.pushError(error, 'math:add', { a: 1, b: 2 });
    expect(harness.query).toEqual({ data: undefined, error, isLoading: false });

    unmount(harness);
  });

  it('replaces the watch when getter arguments change and ignores stale updates', async () => {
    const harness = createHarness(client, doubleRef, { n: 1 });
    await tick();
    const first = client.watchCalls[0]!;

    flushSync(() => harness.setArgs({ n: 2 }));
    expect(harness.query).toEqual({ data: undefined, error: null, isLoading: true });
    expect(client.watchCalls).toHaveLength(2);
    expect(client.watchCalls[1]!.args).toEqual({ n: 2 });

    first.options.onUpdate({ data: 999, error: null, isLoading: false });
    expect(harness.query.data).not.toBe(999);

    client.push({ data: 4, error: null, isLoading: false }, 'math:double', { n: 2 });
    expect(harness.query).toEqual({ data: 4, error: null, isLoading: false });

    unmount(harness);
  });

  it('does not watch while skipped and resumes when getter arguments become valid', async () => {
    const harness = createHarness(client, addRef, 'skip');
    await tick();

    expect(harness.query).toEqual({ data: undefined, error: null, isLoading: false });
    expect(client.watchCalls).toHaveLength(0);

    flushSync(() => harness.setArgs({ a: 2, b: 3 }));
    expect(client.watchCalls).toHaveLength(1);
    client.push({ data: { sum: 5 }, error: null, isLoading: false }, 'math:add', { a: 2, b: 3 });
    expect(harness.query.data).toEqual({ sum: 5 });

    unmount(harness);
  });

  it('unsubscribes when its owning component is destroyed', async () => {
    const harness = createHarness(client);
    await tick();

    expect(client.subscriptions.size).toBe(1);
    unmount(harness);
    expect(client.subscriptions.size).toBe(0);
  });
});
