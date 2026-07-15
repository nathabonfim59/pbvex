import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import assert from 'node:assert/strict';
import { canonicalJson } from './canonical.js';

test('shared canonical number fixture', async () => {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../..');
  const vectors = JSON.parse(await readFile(path.join(root, 'fixtures/canonical/numbers.json'), 'utf8')) as Array<{value:number;canonical:string}>;
  for (const vector of vectors) assert.equal(canonicalJson(vector.value), vector.canonical);
});
