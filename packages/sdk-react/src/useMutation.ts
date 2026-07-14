import { useCallback } from 'react';
import type { FunctionReference, ArgsOf } from '@pbvex/sdk-core';
import { usePBVexClient } from './useClient.js';
import type { UseCallable } from './types.js';

export function useMutation<Ref extends FunctionReference<'mutation', any, any>>(
  ref: Ref,
): UseCallable<Ref> {
  const client = usePBVexClient();
  return useCallback(
    (args?: ArgsOf<Ref>) => (client.mutation as any)(ref, args),
    [client, ref],
  ) as UseCallable<Ref>;
}
