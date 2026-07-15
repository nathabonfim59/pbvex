import assert from 'node:assert/strict';
import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { resolveServerBinary } from '../dist/index.js';

test('supports an explicit server binary override', async () => {
  const dir = await mkdtemp(path.join(tmpdir(), 'pbvex-server-'));
  const binary = path.join(dir, process.platform === 'win32' ? 'pbvex.exe' : 'pbvex');
  await writeFile(binary, 'test');
  if (process.platform !== 'win32') await chmod(binary, 0o755);
  try {
    assert.equal(resolveServerBinary({ env: { PBVEX_SERVER_BINARY: binary } }), binary);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});
