import type {
  FunctionReference,
  OptionalRestArgs,
  ArgsAndOptions,
  EmptyObject,
} from '@pbvex/protocol';

// `any` args: callers may pass anything or omit it.
declare const anyArgsRef: FunctionReference<'query', any, any>;
const anyArgsSupplied: OptionalRestArgs<typeof anyArgsRef> = [{ a: 1 }] as any;
const anyArgsOmitted: OptionalRestArgs<typeof anyArgsRef> = [];
const anyArgsAndOpts: ArgsAndOptions<typeof anyArgsRef> = [{ a: 1 }, { foo: 1 }] as any;

// `void` args: nothing may be supplied at the call site.
declare const voidRef: FunctionReference<'query', void, number>;
const voidCall: OptionalRestArgs<typeof voidRef> = [];
const voidOpts: ArgsAndOptions<typeof voidRef> = [];
// @ts-expect-error void args do not accept a payload
const voidBad: OptionalRestArgs<typeof voidRef> = [{ x: 1 }];

// `undefined` args behave like void.
declare const undefRef: FunctionReference<'query', undefined, number>;
const undefCall: OptionalRestArgs<typeof undefRef> = [];

// EmptyObject args: omittable, or supply an empty object.
declare const emptyRef: FunctionReference<'query', EmptyObject, number>;
const emptyOmitted: OptionalRestArgs<typeof emptyRef> = [];
const emptySupplied: OptionalRestArgs<typeof emptyRef> = [{}];
const emptyOptsOmitted: ArgsAndOptions<typeof emptyRef> = [];

// All-optional-but-nonempty args are omittable via empty-object assignability.
declare const optionalRef: FunctionReference<'query', { filter?: string }, number>;
const optionalOmitted: OptionalRestArgs<typeof optionalRef> = [];
const optionalSupplied: OptionalRestArgs<typeof optionalRef> = [{ filter: 'x' }];

// Required args must be supplied exactly once.
declare const requiredRef: FunctionReference<'query', { id: string }, number>;
const requiredCall: OptionalRestArgs<typeof requiredRef> = [{ id: 'x' }];
// @ts-expect-error required args cannot be omitted
const requiredOmitted: OptionalRestArgs<typeof requiredRef> = [];
// @ts-expect-error missing required field
const requiredMissing: OptionalRestArgs<typeof requiredRef> = [{}];

// ArgsAndOptions retains the options tuple for required args.
interface MyOptions {
  timeoutMs?: number;
}
const requiredOpts: ArgsAndOptions<typeof requiredRef, MyOptions> = [{ id: 'x' }, { timeoutMs: 100 }];
const requiredOptsOnly: ArgsAndOptions<typeof requiredRef, MyOptions> = [{ id: 'x' }];
// @ts-expect-error options must match the supplied Options shape
const requiredBadOpts: ArgsAndOptions<typeof requiredRef, MyOptions> = [{ id: 'x' }, { timeoutMs: 'no' }];

export {
  anyArgsSupplied,
  anyArgsOmitted,
  anyArgsAndOpts,
  voidCall,
  voidOpts,
  voidBad,
  undefCall,
  emptyOmitted,
  emptySupplied,
  emptyOptsOmitted,
  optionalOmitted,
  optionalSupplied,
  requiredCall,
  requiredOmitted,
  requiredMissing,
  requiredOpts,
  requiredOptsOnly,
  requiredBadOpts,
};
