import { expectTypeOf } from 'vitest';
import { v } from '../src/runtime/values.js';
import type { ObjectValidator, Validator } from '../src/runtime/values.js';
import { paginationResultValidator, type Id } from '../src/runtime/server.js';

type OutputOf<T extends Validator<any, any>> = T['__type'];
type InputOf<T extends Validator<any, any>> = NonNullable<T['__inputType']>;

const userId = v.id('users').validate('u1');
expectTypeOf(userId).toEqualTypeOf<Id<'users'>>();

// @ts-expect-error
const badId: Id<'messages'> = userId;

expectTypeOf(v.null().validate(null)).toEqualTypeOf<null>();
expectTypeOf(v.int64().validate(1n)).toEqualTypeOf<bigint>();
expectTypeOf(v.float64().validate(1.5)).toEqualTypeOf<number>();
expectTypeOf(v.number().validate(0)).toEqualTypeOf<number>();
expectTypeOf(v.boolean().validate(true)).toEqualTypeOf<boolean>();
expectTypeOf(v.string().validate('x')).toEqualTypeOf<string>();
// @ts-expect-error
const n: number = v.string().validate('x');
expectTypeOf(v.bytes().validate(new ArrayBuffer(0))).toEqualTypeOf<ArrayBuffer>();
expectTypeOf(v.any().validate('x')).toEqualTypeOf<any>();

expectTypeOf(v.literal('ok').validate('ok')).toEqualTypeOf<'ok'>();
expectTypeOf(v.literal(42).validate(42)).toEqualTypeOf<42>();
expectTypeOf(v.literal(true).validate(true)).toEqualTypeOf<true>();
expectTypeOf(v.literal(42n).validate(42n)).toEqualTypeOf<42n>();

const object = v.object({ name: v.string(), channel: v.optional(v.string()) }).validate({ name: 'a' });
expectTypeOf(object).toMatchTypeOf<{ name: string; channel?: string | undefined }>();
// @ts-expect-error
const channel: string = object.channel;

expectTypeOf(v.array(v.string()).validate(['a'])).toEqualTypeOf<string[]>();
const rec = v.record(v.literal('foo'), v.string()).validate({ foo: 'bar' });
expectTypeOf(rec).toEqualTypeOf<Record<'foo', string>>();
// @ts-expect-error
rec.bar;

const recByString = v.record(v.string(), v.string()).validate({ a: 'b' });
expectTypeOf(recByString).toEqualTypeOf<Record<string, string>>();

expectTypeOf(v.union(v.literal('a'), v.literal('b')).validate('a')).toEqualTypeOf<'a' | 'b'>();
expectTypeOf(v.optional(v.string()).validate(undefined)).toEqualTypeOf<string | undefined>();
// @ts-expect-error
const opt: string = v.optional(v.string()).validate('x');

const baseObject = v.object({
  name: v.string(),
  count: v.number(),
  note: v.optional(v.string()),
  enabled: v.defaulted(v.boolean(), true),
});
const extendedObject = baseObject.extend({ count: v.string(), extra: v.literal('extra') });
expectTypeOf(extendedObject).toMatchTypeOf<ObjectValidator>();
expectTypeOf<OutputOf<typeof extendedObject>>().toEqualTypeOf<
  { name: string; count: string; note: string | undefined; enabled: boolean; extra: 'extra' }
>();
type ExtendedInput = { name: string; count: string; note?: string; enabled?: boolean; extra: 'extra' };
expectTypeOf<InputOf<typeof extendedObject>>().toMatchTypeOf<ExtendedInput>();
expectTypeOf<ExtendedInput>().toMatchTypeOf<InputOf<typeof extendedObject>>();

const pickedObject = extendedObject.pick('extra', 'name', 'note');
expectTypeOf<OutputOf<typeof pickedObject>>().toEqualTypeOf<
  { extra: 'extra'; name: string; note: string | undefined }
>();
type PickedInput = { extra: 'extra'; name: string; note?: string };
expectTypeOf<InputOf<typeof pickedObject>>().toMatchTypeOf<PickedInput>();
expectTypeOf<PickedInput>().toMatchTypeOf<InputOf<typeof pickedObject>>();

const omittedObject = extendedObject.omit('name', 'extra');
expectTypeOf<OutputOf<typeof omittedObject>>().toEqualTypeOf<
  { count: string; note: string | undefined; enabled: boolean }
>();
type OmittedInput = { count: string; note?: string; enabled?: boolean };
expectTypeOf<InputOf<typeof omittedObject>>().toMatchTypeOf<OmittedInput>();
expectTypeOf<OmittedInput>().toMatchTypeOf<InputOf<typeof omittedObject>>();

const partialObject = baseObject.partial();
expectTypeOf<OutputOf<typeof partialObject>>().toEqualTypeOf<
  { name: string | undefined; count: number | undefined; note: string | undefined; enabled: boolean | undefined }
>();
type PartialInput = { name?: string; count?: number; note?: string; enabled?: boolean };
expectTypeOf<InputOf<typeof partialObject>>().toMatchTypeOf<PartialInput>();
expectTypeOf<PartialInput>().toMatchTypeOf<InputOf<typeof partialObject>>();

// @ts-expect-error object composition only accepts fields from the current shape
baseObject.pick('missing');
// @ts-expect-error object composition only accepts fields from the current shape
baseObject.omit('missing');

const paginationItem = v.object({
  name: v.string(),
  count: v.defaulted(v.number(), 0),
}).extend({ selected: v.boolean() });
const paginationResult = paginationResultValidator(paginationItem);
expectTypeOf<OutputOf<typeof paginationResult>>().toEqualTypeOf<{
  page: Array<{ name: string; count: number; selected: boolean }>;
  isDone: boolean;
  continueCursor: string;
}>();
type PaginationInput = {
  page: Array<{ name: string; count?: number; selected: boolean }>;
  isDone: boolean;
  continueCursor: string;
};
expectTypeOf<InputOf<typeof paginationResult>>().toMatchTypeOf<PaginationInput>();
expectTypeOf<PaginationInput>().toMatchTypeOf<InputOf<typeof paginationResult>>();
