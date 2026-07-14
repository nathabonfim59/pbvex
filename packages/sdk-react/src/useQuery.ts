import { useCallback, useMemo, useSyncExternalStore } from 'react';
import type { FunctionReference, ArgsOf, ReturnOf, QueryResult } from '@pbvex/sdk-core';
import { usePBVexClient } from './useClient.js';
import { QueryStore, toCanonical } from './store.js';
import type { UseArgs } from './types.js';

export function useQueryResult<Ref extends FunctionReference<'query', any, any, 'public'>>(
  ref: Ref,
  ...args: UseArgs<Ref>
): QueryResult<ReturnOf<Ref>> {
  const client = usePBVexClient();
  const path = ref._path;
  const [argsOrSkip] = args;
  const isSkip = argsOrSkip === 'skip';
  const canonical = useMemo(() => (isSkip ? 'skip' : toCanonical(argsOrSkip)), [isSkip, argsOrSkip]);

  const store = useMemo(
    () => new QueryStore<ArgsOf<Ref>, ReturnOf<Ref>>(client, path, isSkip ? 'skip' : argsOrSkip),
    [client, path, canonical, isSkip],
  );

  const subscribe = useCallback((callback: () => void) => store.subscribe(callback), [store]);
  const getSnapshot = useCallback(() => store.getSnapshot(), [store]);
  const getServerSnapshot = useCallback(() => store.getServerSnapshot(), [store]);

  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

export function useQuery<Ref extends FunctionReference<'query', any, any, 'public'>>(
  ref: Ref,
  ...args: UseArgs<Ref>
): ReturnOf<Ref> | undefined {
  const result = useQueryResult(ref, ...args);
  if (result.error) {
    throw result.error;
  }
  if (result.isLoading) {
    return undefined;
  }
  return result.data;
}
