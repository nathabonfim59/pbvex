import type { FunctionReference, ReturnOf } from '@pbvex/sdk-core';
import type { UseArgs } from './types.js';
/**
 * Backwards-compatible alias for useQuery.
 */
export declare function useSubscription<Ref extends FunctionReference<'query', any, any, 'public'>>(ref: Ref, ...args: UseArgs<Ref>): ReturnOf<Ref> | undefined;
//# sourceMappingURL=useSubscription.d.ts.map