import { Client, type FunctionReference, type ArgsOf, type ReturnOf, type IsOptionalArgs } from '@pbvex/client';
import { getClient } from './client.js';

type MutationCallable<Args, Return> = IsOptionalArgs<Args> extends true
  ? (args?: Args) => Promise<Return>
  : (args: Args) => Promise<Return>;

export function useMutation<Ref extends FunctionReference<'mutation', any, any>>(
  ref: Ref,
  client?: Client,
): MutationCallable<ArgsOf<Ref>, ReturnOf<Ref>> {
  const resolved = client ?? getClient();
  return ((args?: ArgsOf<Ref>) => (resolved.mutation as any)(ref, args)) as MutationCallable<ArgsOf<Ref>, ReturnOf<Ref>>;
}

export function useAction<Ref extends FunctionReference<'action', any, any>>(
  ref: Ref,
  client?: Client,
): MutationCallable<ArgsOf<Ref>, ReturnOf<Ref>> {
  const resolved = client ?? getClient();
  return ((args?: ArgsOf<Ref>) => (resolved.action as any)(ref, args)) as MutationCallable<ArgsOf<Ref>, ReturnOf<Ref>>;
}
