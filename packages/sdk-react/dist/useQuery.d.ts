import type { FunctionReference, ReturnOf, QueryResult } from '@pbvex/sdk-core';
import type { UseArgs } from './types.js';
export declare function useQueryResult<Ref extends FunctionReference<'query', any, any, 'public'>>(ref: Ref, ...args: UseArgs<Ref>): QueryResult<ReturnOf<Ref>>;
export declare function useQuery<Ref extends FunctionReference<'query', any, any, 'public'>>(ref: Ref, ...args: UseArgs<Ref>): ReturnOf<Ref> | undefined;
//# sourceMappingURL=useQuery.d.ts.map