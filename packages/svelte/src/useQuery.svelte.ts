import {
  Client,
  type ArgsOf,
  type FunctionReference,
  type QueryResult,
  type ReturnOf,
} from '@pbvex/client';
import { getClient } from './client.js';

/** Pass this value (or return it from an args getter) to disable a query. */
export const skip = 'skip' as const;
export type Skip = typeof skip;

export type QueryState<Data> = Readonly<QueryResult<Data>>;

export type QueryArgs<Args> = Args | Skip | (() => Args | Skip);

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

type UseQueryArgs<Args> = EmptyArgs<Args> extends true
  ? [argsOrClient?: QueryArgs<Args> | Client, client?: Client]
  : [args: QueryArgs<Args>, client?: Client];

function isSkip(value: unknown): value is Skip {
  return value === skip;
}

function resolveClientAndArgs<Args>(
  argsOrClient: QueryArgs<Args> | Client | undefined,
  maybeClient: Client | undefined,
): { getArgs: () => Args | Skip | undefined; client: Client } {
  const client =
    maybeClient ?? (argsOrClient instanceof Client ? argsOrClient : undefined) ?? getClient();
  const args = argsOrClient instanceof Client ? undefined : argsOrClient;

  return {
    client,
    getArgs: typeof args === 'function' ? args as () => Args | Skip : () => args,
  };
}

/**
 * Creates component-scoped reactive query state.
 *
 * Pass a getter when arguments depend on reactive component state:
 * `useQuery(api.messages.list, () => ({ channel }))`.
 * The watch is replaced when the getter's value changes and is automatically
 * unsubscribed when the owning component is destroyed.
 */
export function useQuery<Ref extends FunctionReference<'query', any, any>>(
  ref: Ref,
  ...args: UseQueryArgs<ArgsOf<Ref>>
): QueryState<ReturnOf<Ref>>;
export function useQuery<Ref extends FunctionReference<'query', any, any>>(
  ref: Ref,
  argsOrClient?: QueryArgs<ArgsOf<Ref>> | Client,
  maybeClient?: Client,
): QueryState<ReturnOf<Ref>> {
  const { client, getArgs } = resolveClientAndArgs<ArgsOf<Ref>>(argsOrClient, maybeClient);
  let state = $state<QueryResult<ReturnOf<Ref>>>({
    data: undefined,
    error: null,
    isLoading: true,
  });

  $effect(() => {
    const currentArgs = getArgs();
    state.data = undefined;
    state.error = null;

    if (isSkip(currentArgs)) {
      state.isLoading = false;
      return;
    }

    state.isLoading = true;
    let active = true;
    const unsubscribe = (client.watch as any)(ref, currentArgs, {
      onUpdate: (next: QueryResult<ReturnOf<Ref>>) => {
        if (!active) return;
        state.data = next.data;
        state.error = next.error;
        state.isLoading = next.isLoading;
      },
      onError: (error: Error) => {
        if (!active) return;
        state.data = undefined;
        state.error = error;
        state.isLoading = false;
      },
    });

    return () => {
      active = false;
      unsubscribe();
    };
  });

  return state;
}

/** @deprecated Renamed to useQuery; it now returns rune-backed query state. */
export const useQueryResult = useQuery;

/** @deprecated Renamed to useQuery; it now returns rune-backed query state. */
export const useSubscription = useQuery;

/** @deprecated Renamed to useQuery; it now returns rune-backed query state. */
export const createQuery = useQuery;
