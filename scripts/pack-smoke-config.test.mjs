import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

import { createConsumerManifest } from './pack-smoke-config.mjs';

test('the CLI declares TypeScript as a runtime dependency', async () => {
  const packageJson = JSON.parse(
    await readFile(new URL('../packages/pbvex/package.json', import.meta.url), 'utf8'),
  );

  assert.equal(packageJson.dependencies.typescript, '^5.0.0');
  assert.equal(packageJson.devDependencies.typescript, undefined);
});

test('the candidate protocol tarball override survives package version bumps', () => {
  const protocol = '/tmp/pbvex-protocol-9.8.7.tgz';
  const manifest = createConsumerManifest({
    protocol,
    pbvex: '/tmp/pbvex-9.8.7.tgz',
    client: '/tmp/pbvex-client-9.8.7.tgz',
    react: '/tmp/pbvex-react-9.8.7.tgz',
    svelte: '/tmp/pbvex-svelte-9.8.7.tgz',
  });

  assert.equal(manifest.dependencies['@pbvex/protocol'], `file:${protocol}`);
  assert.deepEqual(manifest.pnpm.overrides, {
    '@pbvex/protocol': `file:${protocol}`,
  });
  assert.equal(Object.keys(manifest.pnpm.overrides).some((key) => key.includes('@0.1.0')), false);
});
