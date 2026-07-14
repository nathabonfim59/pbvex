import assert from 'node:assert/strict';
import test from 'node:test';
import { runInNewContext } from 'node:vm';
import { isJsonValue, isPlainObject } from './validators.js';

test('plain JSON validation accepts ordinary records across supported prototypes', () => {
  const crossRealm = runInNewContext('({ nested: { ok: true } })') as unknown;
  const nullPrototype = Object.assign(Object.create(null) as Record<string, unknown>, { ok: true });

  assert.equal(isPlainObject(crossRealm), true);
  assert.equal(isJsonValue(crossRealm), true);
  assert.equal(isPlainObject(nullPrototype), true);
  assert.equal(isJsonValue(nullPrototype), true);
});

test('plain JSON validation rejects branded and constructed objects', () => {
  class Example {
    ok = true;
  }

  assert.equal(isJsonValue(new Date()), false);
  assert.equal(isJsonValue(new Example()), false);
});

test('plain object detection rejects a constructor-name spoof', () => {
  const prototype = Object.create(null) as Record<string, unknown>;
  Object.defineProperty(prototype, 'constructor', {
    value: function Object() {},
    configurable: true,
  });
  const spoof = Object.assign(Object.create(prototype) as Record<string, unknown>, { ok: true });

  assert.equal(isPlainObject(spoof), false);
  assert.equal(isJsonValue(spoof), false);
});
