import { derived, type Readable, type Subscriber, type Unsubscriber } from 'svelte/store';
import {
  Client,
  type FunctionReference,
  type ArgsOf,
  type ReturnOf,
  type QueryResult,
} from '@pbvex/sdk-core';
import { encodeValue, canonicalJson, type PbvexValue } from '@pbvex/protocol';
import { getClient } from './client.js';

export const skip = 'skip' as const;
export type Skip = typeof skip;

export type EmptyObject = Record<string, never>;

type IsAny<Args> = 0 extends (1 & Args) ? true : false;

type EmptyArgs<Args> = IsAny<Args> extends true
  ? false
  : [Args] extends [undefined | void]
    ? true
    : Args extends EmptyObject
      ? true
      : {} extends Args
        ? true
        : false;

type ArgsOrSkip<Args> = Args | Skip;
type ArgsInput<Args> =
  | ArgsOrSkip<Args>
  | Readable<ArgsOrSkip<Args>>
  | (EmptyArgs<Args> extends true ? Client : never);

type UseQueryArgs<Args> = EmptyArgs<Args> extends true
  ? [argsOrClient?: ArgsInput<Args>, client?: Client]
  : [argsOrClient: ArgsInput<Args>, client?: Client];

function isReadable(value: unknown): value is Readable<unknown> {
  return (
    value !== null &&
    typeof value === 'object' &&
    'subscribe' in value &&
    typeof (value as { subscribe?: unknown }).subscribe === 'function'
  );
}

function isSkip(value: unknown): value is Skip {
  return value === 'skip';
}

function normalizeArgs<Args>(args: ArgsInput<Args> | undefined): Readable<ArgsOrSkip<Args>> {
  if (args === undefined || isSkip(args) || !isReadable(args)) {
    return { subscribe: (fn) => { fn(args as ArgsOrSkip<Args>); return () => {}; } };
  }
  return args;
}

function resolveClientAndArgs<Args>(
  argsOrClient: ArgsInput<Args> | Client | undefined,
  maybeClient: Client | undefined,
): { args: ArgsInput<Args> | undefined; client: Client } {
  const client =
    maybeClient ?? (argsOrClient instanceof Client ? argsOrClient : undefined) ?? getClient();
  const args = argsOrClient instanceof Client ? undefined : argsOrClient;
  return { args, client };
}

function defaultEncodeArgs(args: unknown): ReturnType<typeof encodeValue> {
  return args === undefined ? ({} as ReturnType<typeof encodeValue>) : (encodeValue(args as PbvexValue) as ReturnType<typeof encodeValue>);
}

function argsKey(args: unknown): string {
  return canonicalJson(defaultEncodeArgs(args));
}

function createQueryResultStore<Ref extends FunctionReference<'query', any, any>>(
  ref: Ref,
  args: ArgsInput<ArgsOf<Ref>> | undefined,
  client: Client,
): Readable<QueryResult<ReturnOf<Ref>>> {
  const argsStore = normalizeArgs(args);
  const initialResult: QueryResult<ReturnOf<Ref>> = { data: undefined, error: null, isLoading: true };

  let value: QueryResult<ReturnOf<Ref>> = initialResult;
  const subscribers = new Set<Subscriber<QueryResult<ReturnOf<Ref>>>>();
  let generation = 0;
  let lastKey: string | undefined;
  let argsUnsubscribe: Unsubscriber | undefined;
  let watchUnsubscribe: Unsubscriber | undefined;

  const set = (next: QueryResult<ReturnOf<Ref>>) => {
    if (next !== value) {
      value = next;
      for (const fn of subscribers) {
        fn(value);
      }
    }
  };

  function start() {
    argsUnsubscribe = argsStore.subscribe((currentArgs) => {
      const key = isSkip(currentArgs) ? 'skip' : argsKey(currentArgs);
      if (key === lastKey) return;
      lastKey = key;

      // Invalidate any callbacks from the previous watch before tearing it down.
      ++generation;
      if (watchUnsubscribe) {
        watchUnsubscribe();
        watchUnsubscribe = undefined;
      }

      set(initialResult);

      if (isSkip(currentArgs)) {
        set({ data: undefined, error: null, isLoading: false });
        return;
      }

      const g = generation;
      watchUnsubscribe = (client.watch as any)(ref, currentArgs, {
        onUpdate: (result: QueryResult<ReturnOf<Ref>>) => {
          if (g !== generation) return;
          set(result);
        },
        onError: (error: Error) => {
          if (g !== generation) return;
          set({ data: undefined, error, isLoading: false });
        },
      });
    });
  }

  function stop() {
    ++generation;
    argsUnsubscribe?.();
    watchUnsubscribe?.();
    argsUnsubscribe = undefined;
    watchUnsubscribe = undefined;
    lastKey = undefined;
  }

  return {
    subscribe(fn: Subscriber<QueryResult<ReturnOf<Ref>>>, invalidate?: (value?: QueryResult<ReturnOf<Ref>>) => void): Unsubscriber {
      subscribers.add(fn);
      if (subscribers.size === 1) start();
      fn(value);
      return () => {
        subscribers.delete(fn);
        if (subscribers.size === 0) stop();
      };
    },
  };
}

export function useQueryResult<Ref extends FunctionReference<'query', any, any>>(
  ref: Ref,
  ...args: UseQueryArgs<ArgsOf<Ref>>
): Readable<QueryResult<ReturnOf<Ref>>>;
export function useQueryResult<Ref extends FunctionReference<'query', any, any>>(
  ref: Ref,
  argsOrClient?: ArgsInput<ArgsOf<Ref>> | Client,
  maybeClient?: Client,
): Readable<QueryResult<ReturnOf<Ref>>> {
  const { args, client } = resolveClientAndArgs<ArgsOf<Ref>>(argsOrClient, maybeClient);
  return createQueryResultStore(ref, args, client);
}

export function useQuery<Ref extends FunctionReference<'query', any, any>>(
  ref: Ref,
  ...args: UseQueryArgs<ArgsOf<Ref>>
): Readable<ReturnOf<Ref> | undefined>;
export function useQuery<Ref extends FunctionReference<'query', any, any>>(
  ref: Ref,
  argsOrClient?: ArgsInput<ArgsOf<Ref>> | Client,
  maybeClient?: Client,
): Readable<ReturnOf<Ref> | undefined> {
  const { args, client } = resolveClientAndArgs<ArgsOf<Ref>>(argsOrClient, maybeClient);
  return derived(createQueryResultStore(ref, args, client), (result) => result.data);
}

export const createQuery = useQuery;

export const useSubscription = useQueryResult;
