import { test } from 'node:test';
import assert from 'node:assert';
import { runInNewContext } from 'node:vm';
import { canonicalJson, hashSha256, hashSha256Bytes, canonicalHash } from './canonical.js';
import type { JSONValue } from './types.js';

test('canonicalJson: sorts object keys', () => {
  assert.strictEqual(canonicalJson({ b: 2, a: 1 }), '{"a":1,"b":2}');
});

test('canonicalJson: no whitespace', () => {
  assert.strictEqual(canonicalJson([1, 2, 3]), '[1,2,3]');
  assert.strictEqual(canonicalJson({ x: 'y' }), '{"x":"y"}');
});

test('canonicalJson: escapes strings deterministically', () => {
  assert.strictEqual(canonicalJson({ key: 'a\nb' }), '{"key":"a\\nb"}');
  assert.strictEqual(canonicalJson({ key: '"quote"' }), '{"key":"\\"quote\\""}');
});

test('canonicalJson: rejects non-finite numbers', () => {
  assert.throws(() => canonicalJson(NaN), /Non-finite/);
  assert.throws(() => canonicalJson(Infinity), /Non-finite/);
});

test('canonicalJson: rejects undefined values', () => {
  assert.throws(() => canonicalJson({ a: undefined } as unknown as JSONValue), /undefined/);
  assert.throws(() => canonicalJson([undefined] as unknown as JSONValue), /undefined/);
});

test('canonicalJson: rejects cyclic objects', () => {
  const a: unknown[] = [];
  a.push(a);
  assert.throws(() => canonicalJson(a as unknown as JSONValue), /Cyclic/);

  const o: Record<string, unknown> = {};
  o.self = o;
  assert.throws(() => canonicalJson(o as unknown as JSONValue), /Cyclic/);
});

test('canonicalJson: rejects unsafe object prototypes', () => {
  class Box {
    value = 1;
  }
  assert.throws(() => canonicalJson(new Box() as unknown as JSONValue), /Unsupported object prototype/);
  assert.throws(() => canonicalJson(new Date() as unknown as JSONValue), /Unsupported object prototype/);
});

test('canonicalJson: accepts plain records from another realm', () => {
  const record = runInNewContext('({ nested: { b: 2, a: 1 } })') as JSONValue;
  assert.strictEqual(canonicalJson(record), '{"nested":{"a":1,"b":2}}');
  const instance = runInNewContext('new (class Box { constructor() { this.value = 1; } })()') as JSONValue;
  assert.throws(() => canonicalJson(instance), /Unsupported object prototype/);
});

test('canonicalJson: rejects unsupported types', () => {
  assert.throws(() => canonicalJson(BigInt(1) as unknown as JSONValue), /Unsupported value/);
  assert.throws(() => canonicalJson((() => 1) as unknown as JSONValue), /Unsupported value/);
});

test('hashSha256 returns 64-char hex', async () => {
  const hash = await hashSha256('hello');
  assert.strictEqual(hash.length, 64);
  assert.strictEqual(/^[0-9a-f]+$/.test(hash), true);
});

test('hashSha256Bytes returns 64-char hex', async () => {
  const hash = await hashSha256Bytes(new Uint8Array([1, 2, 3]).buffer);
  assert.strictEqual(hash.length, 64);
  assert.strictEqual(/^[0-9a-f]+$/.test(hash), true);
});

test('canonicalHash: same input yields same hash', async () => {
  const h1 = await canonicalHash({ b: 2, a: 1 });
  const h2 = await canonicalHash({ a: 1, b: 2 });
  assert.strictEqual(h1, h2);
});
