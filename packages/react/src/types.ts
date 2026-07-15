import type { FunctionReference } from '@pbvex/client';

export type EmptyObject = Record<string, never>;

/**
 * Detect the `any` type, which must not be treated as omittable args.
 */
export type IsAny<Args> = 0 extends (1 & Args) ? true : false;

/**
 * Detect whether args can be omitted at a call site.
 *
 * Args are omittable when they are `undefined`/`void` or an empty object.
 */
export type IsOptionalArgs<Args> = IsAny<Args> extends true
  ? false
  : [Args] extends [undefined | void]
    ? true
    : Args extends EmptyObject
      ? true
      : {} extends Args
        ? true
        : false;

/**
 * Rest-tuple overload for query hooks: required args need an argument (or
 * 'skip'); optional/empty args can be omitted.
 */
export type UseArgs<Ref> =
  Ref extends FunctionReference<any, infer Args, any, any>
    ? IsOptionalArgs<Args> extends true
      ? [args?: Args | 'skip']
      : [args: Args | 'skip']
    : never;

/**
 * Callable type for useMutation/useAction. Required args must be supplied;
 * optional/empty args can be omitted.
 */
export type UseCallable<Ref> =
  Ref extends FunctionReference<any, infer Args, infer Return, any>
    ? IsOptionalArgs<Args> extends true
      ? (args?: Args) => Promise<Return>
      : (args: Args) => Promise<Return>
    : never;
