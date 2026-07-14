import { vi } from 'vitest';
import { Client, type FunctionReference, type QueryResult, type Unsubscribe } from '@pbvex/sdk-core';
import { encodeValue, canonicalJson, type PbvexValue } from '@pbvex/protocol';

export interface WatchCall {
  path: string;
  args: unknown;
  options: {
    onUpdate: (result: QueryResult<unknown>) => void;
    onError?: (error: Error) => void;
    onConnectionStateChange?: (state: string) => void;
  };
}

function defaultEncodeArgs(args: unknown): ReturnType<typeof encodeValue> {
  return args === undefined ? ({} as ReturnType<typeof encodeValue>) : (encodeValue(args as PbvexValue) as ReturnType<typeof encodeValue>);
}

function argsKey(args: unknown): string {
  return canonicalJson(defaultEncodeArgs(args));
}

export class MockClient extends Client {
  watch = vi.fn(this._watch.bind(this));
  mutation = vi.fn(this._mutation.bind(this));
  action = vi.fn(this._action.bind(this));

  watchCalls: WatchCall[] = [];
  subscriptions = new Map<string, { calls: WatchCall[]; latest: QueryResult<unknown> }>();

  constructor() {
    super('http://localhost:8090', { fetch: vi.fn() as unknown as typeof fetch });
  }

  private _watch(ref: unknown, args?: unknown, options?: unknown): Unsubscribe {
    const path = typeof ref === 'string' ? ref : (ref as { _path: string })._path;
    const key = `${path}:${argsKey(args)}`;
    const call = {
      path,
      args,
      options: options as WatchCall['options'],
    };
    this.watchCalls.push(call);

    let subscription = this.subscriptions.get(key);
    if (!subscription) {
      subscription = { calls: [], latest: { data: undefined, error: null, isLoading: true } };
      this.subscriptions.set(key, subscription);
    }
    subscription.calls.push(call);

    call.options.onUpdate(subscription.latest);

    return () => {
      const index = subscription!.calls.indexOf(call);
      if (index !== -1) subscription!.calls.splice(index, 1);
      if (subscription!.calls.length === 0) {
        this.subscriptions.delete(key);
      }
    };
  }

  private _mutation(_ref: unknown, _args?: unknown): Promise<unknown> {
    return Promise.resolve(undefined);
  }

  private _action(_ref: unknown, _args?: unknown): Promise<unknown> {
    return Promise.resolve(undefined);
  }

  push(result: QueryResult<unknown>, ref?: string, args?: unknown): void {
    const key = this.key(ref, args);
    const subscription = this.subscriptions.get(key);
    if (subscription) {
      subscription.latest = result;
      for (const call of subscription.calls) {
        call.options.onUpdate(result);
      }
    }
  }

  pushError(error: Error, ref?: string, args?: unknown): void {
    const key = this.key(ref, args);
    const subscription = this.subscriptions.get(key);
    if (subscription) {
      subscription.latest = { data: undefined, error, isLoading: false };
      for (const call of subscription.calls) {
        call.options.onUpdate(subscription.latest);
        call.options.onError?.(error);
      }
    }
  }

  private key(ref?: string, args?: unknown): string {
    if (ref === undefined) {
      const first = this.subscriptions.keys().next().value;
      if (!first) throw new Error('No active subscription');
      return first as string;
    }
    return `${ref}:${argsKey(args)}`;
  }
}

export const addRef = {
  _path: 'math:add',
  _type: 'query',
} as FunctionReference<'query', { a: number; b: number }, { sum: number }, 'public'>;

export const doubleRef = {
  _path: 'math:double',
  _type: 'query',
} as FunctionReference<'query', { n: number }, number, 'public'>;

export const emptyRef = {
  _path: 'util:empty',
  _type: 'query',
} as FunctionReference<'query', void, string, 'public'>;
