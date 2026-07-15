import { expectTypeOf } from 'vitest';
import { v } from '../src/runtime/values.js';
import type { Id } from '../src/runtime/server.js';

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
