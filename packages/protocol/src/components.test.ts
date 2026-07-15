import assert from 'node:assert/strict';
import test from 'node:test';
import { validateComponents } from './components.js';
import { formatOpaqueId } from './ids.js';

const hash = '0'.repeat(64);
const mac = new Uint8Array(32);
const definition = {
  componentId: 'def_test',
  modulePaths: ['store.ts'],
  moduleHashes: { 'store.ts': hash },
  args: { type: 'object', shape: { item: { type: 'id', tableName: 'items' } } },
};

function graph(item: string) {
  return { definitions: [definition], mounts: [{ name: 'store', componentId: 'def_test', args: { item } }] };
}

test('component mount ids are structural, table-bound, and v2-only', () => {
  const valid = formatOpaqueId({ version: 2, keyId: 1n, namespace: 'root', table: 'items', raw: 'abcdefghijklmno' }, mac);
  assert.ok(validateComponents(graph(valid)));
  assert.throws(() => validateComponents(graph('not-an-id')));
  const wrongTable = formatOpaqueId({ version: 2, keyId: 1n, namespace: 'root', table: 'other', raw: 'abcdefghijklmno' }, mac);
  assert.throws(() => validateComponents(graph(wrongTable)));
  const legacy = formatOpaqueId({ version: 1, keyId: 1n, namespace: 'legacyRootState', table: 'items', raw: 'abcdefghijklmno' }, mac);
  assert.throws(() => validateComponents(graph(legacy)));
});

test('component module paths cannot enter a descendant mount namespace', () => {
  const parent = {
    componentId: 'def_parent',
    modulePaths: ['child/store.ts'],
    moduleHashes: { 'child/store.ts': hash },
  };
  const child = {
    componentId: 'def_child',
    modulePaths: ['store.ts'],
    moduleHashes: { 'store.ts': hash },
  };
  assert.throws(
    () => validateComponents({
      definitions: [parent, child],
      mounts: [{
        name: 'outer',
        componentId: 'def_parent',
        children: [{ name: 'child', componentId: 'def_child' }],
      }],
    }),
    /collides with descendant mount outer\/child/,
  );
});
