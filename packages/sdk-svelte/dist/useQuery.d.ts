import { type Readable } from 'svelte/store';
import { Client, type FunctionReference, type ArgsOf, type ReturnOf, type QueryResult } from '@pbvex/sdk-core';
export declare const skip: "skip";
export type Skip = typeof skip;
export type EmptyObject = Record<string, never>;
type IsAny<Args> = 0 extends (1 & Args) ? true : false;
type EmptyArgs<Args> = IsAny<Args> extends true ? false : [Args] extends [undefined | void] ? true : Args extends EmptyObject ? true : {} extends Args ? true : false;
type ArgsOrSkip<Args> = Args | Skip;
type ArgsInput<Args> = ArgsOrSkip<Args> | Readable<ArgsOrSkip<Args>> | (EmptyArgs<Args> extends true ? Client : never);
type UseQueryArgs<Args> = EmptyArgs<Args> extends true ? [argsOrClient?: ArgsInput<Args>, client?: Client] : [argsOrClient: ArgsInput<Args>, client?: Client];
export declare function useQueryResult<Ref extends FunctionReference<'query', any, any>>(ref: Ref, ...args: UseQueryArgs<ArgsOf<Ref>>): Readable<QueryResult<ReturnOf<Ref>>>;
export declare function useQuery<Ref extends FunctionReference<'query', any, any>>(ref: Ref, ...args: UseQueryArgs<ArgsOf<Ref>>): Readable<ReturnOf<Ref> | undefined>;
export declare const createQuery: typeof useQuery;
export declare const useSubscription: typeof useQueryResult;
export {};
//# sourceMappingURL=useQuery.d.ts.map