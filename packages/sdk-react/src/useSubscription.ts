import type { FunctionReference, ReturnOf } from '@pbvex/sdk-core';
import { useQuery } from './useQuery.js';
import type { UseArgs } from './types.js';

/**
 * Backwards-compatible alias for useQuery.
 */
export function useSubscription<Ref extends FunctionReference<'query', any, any, 'public'>>(
  ref: Ref,
  ...args: UseArgs<Ref>
): ReturnOf<Ref> | undefined {
  return useQuery(ref, ...args);
}
