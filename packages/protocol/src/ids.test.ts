import assert from 'node:assert/strict';
import test from 'node:test';
import { formatOpaqueId, parseOpaqueId } from './ids.js';

const zeroMac = new Uint8Array(32);
const rootId = formatOpaqueId({
  version: 2,
  keyId: 7n,
  namespace: 'root',
  table: 'messages',
  raw: 'abcdefghijklmno',
}, zeroMac);

test('pbv2 structural golden vector is canonical and table-bound', () => {
  assert.equal(rootId, 'pbv2.7.eyJ2IjoyLCJrIjo3LCJuIjoicm9vdCIsInQiOiJtZXNzYWdlcyIsInIiOiJhYmNkZWZnaGlqa2xtbm8ifQ.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA');
  assert.deepEqual(parseOpaqueId(rootId), {
    version: 2, keyId: 7n, namespace: 'root', table: 'messages', raw: 'abcdefghijklmno', mac: zeroMac,
  });
});

test('opaque IDs reject noncanonical base64 and payload JSON', () => {
  assert.equal(parseOpaqueId(rootId.replace('.AAAAAAAA', '.AAAAAAAA=')), undefined);
  const reordered = btoa('{"k":7,"v":2,"n":"root","t":"messages","r":"abcdefghijklmno"}')
    .replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
  assert.equal(parseOpaqueId(`pbv2.7.${reordered}.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA`), undefined);
});

test('legacy pbv1 is accepted only when explicitly allowed', () => {
  const legacy = formatOpaqueId({ version: 1, keyId: 1n, namespace: 'legacyRootState', table: 'messages', raw: 'abcdefghijklmno' }, zeroMac);
  assert.ok(parseOpaqueId(legacy));
  assert.equal(parseOpaqueId(legacy, false), undefined);
});
