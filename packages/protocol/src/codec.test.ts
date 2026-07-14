import { test } from 'node:test';
import assert from 'node:assert';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { runInNewContext } from 'node:vm';
import { encodeValue, decodeValue, encodeReturnValue, id, type PbvexValue } from './codec.js';
import type { JSONValue } from './types.js';

const goldenPath = fileURLToPath(new URL('../../../fixtures/codec/golden.json', import.meta.url));
const golden = JSON.parse(readFileSync(goldenPath, 'utf8')) as {
  roundtrip: { label: string; value: unknown }[];
  invalidDecode: { label: string; value: unknown }[];
};

test('encodeValue: primitives', () => {
  assert.strictEqual(encodeValue(null), null);
  assert.strictEqual(encodeValue(true), true);
  assert.strictEqual(encodeValue(false), false);
  assert.strictEqual(encodeValue(42), 42);
  assert.strictEqual(encodeValue('hello'), 'hello');
});

test('encodeValue: rejects NaN and Infinity', () => {
  assert.throws(() => encodeValue(NaN), /Non-finite/);
  assert.throws(() => encodeValue(Infinity), /Non-finite/);
  assert.throws(() => encodeValue(-Infinity), /Non-finite/);
});

test('encodeValue: int64', () => {
  assert.deepStrictEqual(encodeValue(BigInt(0)), { $integer: 'AAAAAAAAAAA=' });
  assert.deepStrictEqual(encodeValue(BigInt(1)), { $integer: 'AQAAAAAAAAA=' });
  assert.deepStrictEqual(encodeValue(BigInt(-1)), { $integer: '//////////8=' });
});

test('encodeValue: bytes', () => {
  const buffer = new Uint8Array([0x00, 0xff, 0x42]).buffer;
  assert.deepStrictEqual(encodeValue(buffer), { $bytes: 'AP9C' });
});

test('encodeValue: arrays and objects', () => {
  assert.deepStrictEqual(encodeValue([1, 'two', null]), [1, 'two', null]);
  assert.deepStrictEqual(
    encodeValue({ b: 2, a: 1 }),
    { b: 2, a: 1 } // keys are emitted in insertion order; canonical JSON sorts later
  );
});

test('encodeValue: drops undefined object fields', () => {
  assert.deepStrictEqual(encodeValue({ a: 1, b: undefined }), { a: 1 });
});

test('encodeValue: rejects undefined at root', () => {
  assert.throws(() => encodeValue(undefined as unknown as PbvexValue), /undefined/);
});

test('encodeValue: rejects unsafe object prototype', () => {
  class Box {
    value = 1;
  }
  assert.throws(() => encodeValue(new Box() as unknown as PbvexValue), /Unsupported object prototype/);
});

test('codec accepts plain records from another realm', () => {
  const record = runInNewContext('({ nested: { value: 1 } })');
  assert.deepStrictEqual(encodeValue(record as PbvexValue), { nested: { value: 1 } });
  assert.deepStrictEqual(decodeValue(record as JSONValue), { nested: { value: 1 } });
  const instance = runInNewContext('new (class Box { constructor() { this.value = 1; } })()');
  assert.throws(() => encodeValue(instance as PbvexValue), /Unsupported object prototype/);
  assert.throws(() => decodeValue(instance as JSONValue), /Unsupported object prototype/);
});

test('encodeValue: rejects reserved keys', () => {
  const protoObj = JSON.parse('{"__proto__":1}') as PbvexValue;
  assert.throws(() => encodeValue(protoObj), /Invalid object field name/);
  assert.throws(() => encodeValue({ $tag: 1 } as unknown as PbvexValue), /Invalid object field name/);
});

test('encodeValue: rejects cyclic data', () => {
  const a: PbvexValue[] = [];
  a.push(a);
  assert.throws(() => encodeValue(a), /Cyclic/);

  const o: Record<string, PbvexValue> = {};
  o.self = o;
  assert.throws(() => encodeValue(o), /Cyclic/);
});

test('encodeReturnValue: undefined normalizes to null', () => {
  assert.strictEqual(encodeReturnValue(undefined), null);
});

test('encodeReturnValue: other values encode normally', () => {
  assert.strictEqual(encodeReturnValue(42), 42);
  assert.deepStrictEqual(encodeReturnValue({ ok: true }), { ok: true });
});

test('decodeValue: primitives', () => {
  assert.strictEqual(decodeValue(null), null);
  assert.strictEqual(decodeValue(true), true);
  assert.strictEqual(decodeValue(42), 42);
  assert.strictEqual(decodeValue('hello'), 'hello');
});

test('decodeValue: int64', () => {
  assert.strictEqual(decodeValue({ $integer: 'AQAAAAAAAAA=' } as const), BigInt(1));
  assert.strictEqual(decodeValue({ $integer: 'AAAAAAAAAAA=' } as const), BigInt(0));
});

test('decodeValue: bytes', () => {
  const decoded = decodeValue({ $bytes: 'AP9C' } as const);
  assert.ok(decoded instanceof ArrayBuffer);
  assert.deepStrictEqual(new Uint8Array(decoded as ArrayBuffer), new Uint8Array([0x00, 0xff, 0x42]));
});

test('decodeValue: rejects non-finite runtime numbers', () => {
  assert.throws(() => decodeValue(NaN as unknown as JSONValue), /Non-finite/);
  assert.throws(() => decodeValue(Infinity as unknown as JSONValue), /Non-finite/);
  assert.throws(() => decodeValue(-Infinity as unknown as JSONValue), /Non-finite/);
});

test('decodeValue: rejects invalid field names in JSON', () => {
  assert.throws(() => decodeValue({ $tag: 1 } as const), /Invalid field name/);
});

test('decodeValue: rejects unsafe object prototype', () => {
  class Box {
    value = 1;
  }
  assert.throws(() => decodeValue(new Box() as unknown as JSONValue), /Unsupported object prototype/);
  assert.throws(() => decodeValue(new Date() as unknown as JSONValue), /Unsupported object prototype/);
});

test('decodeValue: rejects cyclic JSON', () => {
  const a: JSONValue[] = [];
  a.push(a as JSONValue);
  assert.throws(() => decodeValue(a as JSONValue), /Cyclic/);

  const o: Record<string, JSONValue> = {};
  o.self = o;
  assert.throws(() => decodeValue(o as JSONValue), /Cyclic/);
});

test('encodeValue and decodeValue are inverses', () => {
  const input: PbvexValue = {
    name: 'test',
    count: BigInt(42),
    data: new Uint8Array([1, 2, 3]).buffer,
    nested: [false, 'x', { a: BigInt(-1) }],
  };
  const encoded = encodeValue(input);
  const decoded = decodeValue(encoded);

  assert.ok(decoded !== null && typeof decoded === 'object' && !Array.isArray(decoded));
  const obj = decoded as Record<string, PbvexValue>;
  assert.strictEqual(obj.name, 'test');
  assert.strictEqual(obj.count, BigInt(42));
  assert.ok(obj.data instanceof ArrayBuffer);
  assert.deepStrictEqual(new Uint8Array(obj.data as ArrayBuffer), new Uint8Array([1, 2, 3]));
  assert.ok(Array.isArray(obj.nested));
  assert.strictEqual(obj.nested[0], false);
  assert.strictEqual(obj.nested[1], 'x');
  const nested2 = obj.nested[2];
  assert.ok(nested2 !== null && typeof nested2 === 'object' && !Array.isArray(nested2));
  assert.strictEqual((nested2 as Record<string, PbvexValue>).a, BigInt(-1));
});

test('id: validates and brands a backend-shaped id', () => {
	const value = 'pbv2.7.eyJ2IjoyLCJrIjo3LCJuIjoicm9vdCIsInQiOiJ1c2VycyIsInIiOiJhYmNkZWZnaGlqa2xtbm8ifQ.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA';
  const userId = id('users', value);
  assert.strictEqual(userId, value);
  assert.strictEqual(typeof userId, 'string');
	assert.throws(() => id('users', 'u123'));
});

test('golden roundtrip vectors', () => {
  for (const vector of golden.roundtrip) {
    const decoded = decodeValue(vector.value as JSONValue);
    const encoded = encodeValue(decoded);
    assert.deepStrictEqual(encoded, vector.value, `roundtrip failed for ${vector.label}`);
  }
});

test('golden invalid decode vectors', () => {
  for (const vector of golden.invalidDecode) {
    assert.throws(() => decodeValue(vector.value as JSONValue), `expected ${vector.label} to fail decode`);
  }
});
