import { Client, type FunctionReference, type ArgsOf, type ReturnOf, type IsOptionalArgs } from '@pbvex/sdk-core';
type MutationCallable<Args, Return> = IsOptionalArgs<Args> extends true ? (args?: Args) => Promise<Return> : (args: Args) => Promise<Return>;
export declare function useMutation<Ref extends FunctionReference<'mutation', any, any>>(ref: Ref, client?: Client): MutationCallable<ArgsOf<Ref>, ReturnOf<Ref>>;
export declare function useAction<Ref extends FunctionReference<'action', any, any>>(ref: Ref, client?: Client): MutationCallable<ArgsOf<Ref>, ReturnOf<Ref>>;
export {};
//# sourceMappingURL=useMutation.d.ts.map