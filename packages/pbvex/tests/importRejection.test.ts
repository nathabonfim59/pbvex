import { describe, it, expect } from 'vitest';
import { mkdtemp, rm, mkdir, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { bundle } from '../src/bundler/bundler.js';

describe('import rejection', () => {
  it('rejects an arbitrary npm package import', async () => {
    const tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-import-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `export default { defaultTarget: 'local', targets: { local: { url: 'http://localhost:8090', metadata: {} } } };\n`,
      'utf-8',
    );
    await writeFile(
      path.join(pbvexDir, 'evil.ts'),
      `import { query } from 'pbvex/server';\nimport { v } from 'pbvex/values';\nimport lodash from 'lodash';\nexport const get = query({ args: { channel: v.string() }, returns: v.array(v.string()), handler: async () => [] });\n`,
      'utf-8',
    );
    const result = await bundle({ rootDir: tempDir, target: 'local' });
    await rm(tempDir, { recursive: true, force: true });
    expect(result.diagnostics.length).toBeGreaterThan(0);
    expect(result.diagnostics.some((d) => d.toLowerCase().includes('lodash'))).toBe(true);
  });

  it('rejects node built-in imports', async () => {
    const tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-import-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `export default { defaultTarget: 'local', targets: { local: { url: 'http://localhost:8090', metadata: {} } } };\n`,
      'utf-8',
    );
    await writeFile(
      path.join(pbvexDir, 'messages.ts'),
      `import { query } from 'pbvex/server';\nimport { v } from 'pbvex/values';\nimport { env } from 'node:process';\nexport const get = query({ args: { channel: v.string() }, returns: v.array(v.string()), handler: async () => [] });\n`,
      'utf-8',
    );
    const result = await bundle({ rootDir: tempDir, target: 'local' });
    await rm(tempDir, { recursive: true, force: true });
    expect(result.diagnostics.length).toBeGreaterThan(0);
    expect(result.diagnostics.some((d) => d.toLowerCase().includes('node:process'))).toBe(true);
  });

  it('allows pbvex runtime imports and relative imports', async () => {
    const tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-import-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `export default { defaultTarget: 'local', targets: { local: { url: 'http://localhost:8090', metadata: {} } } };\n`,
      'utf-8',
    );
    await writeFile(
      path.join(pbvexDir, 'helpers.ts'),
      `export function helper(x: string): string { return x; }\n`,
      'utf-8',
    );
    await writeFile(
      path.join(pbvexDir, 'messages.ts'),
      `import { query } from 'pbvex/server';\nimport { v } from 'pbvex/values';\nimport { helper } from './helpers';\nexport const get = query({ args: { channel: v.string() }, returns: v.array(v.string()), handler: async () => [helper('ok')] });\n`,
      'utf-8',
    );
    const result = await bundle({ rootDir: tempDir, target: 'local' });
    await rm(tempDir, { recursive: true, force: true });
    expect(result.diagnostics).toEqual([]);
  });
});
