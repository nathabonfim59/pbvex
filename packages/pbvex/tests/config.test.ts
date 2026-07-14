import { describe, it, expect, afterEach } from 'vitest';
import { mkdtemp, rm, mkdir, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { loadConfig, resolveToken } from '../src/config/config.js';

describe('config', () => {
  let tempDir: string;

  afterEach(async () => {
    if (tempDir) await rm(tempDir, { recursive: true, force: true });
    delete process.env.PBVEX_TOKEN;
    delete process.env.PBVEX_LOCAL_TOKEN;
  });

  it('loads and resolves a real .ts config via safe AST parsing', async () => {
    tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-config-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `export default {\n  project: 'test',\n  defaultTarget: 'local',\n  targets: { local: { url: 'http://localhost:8090', metadata: {} } },\n};\n`,
      'utf-8',
    );
    const config = await loadConfig(tempDir);
    expect(config.project).toBe('test');
    expect(config.defaultTarget).toBe('local');
    expect(config.target).toBe('local');
    expect(config.url).toBe('http://localhost:8090');
  });

  it('allows a defineConfig wrapper', async () => {
    tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-config-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `export default defineConfig({\n  project: 'wrapped',\n  defaultTarget: 'local',\n  targets: { local: { url: 'http://localhost:8090', metadata: {} } },\n});\n`,
      'utf-8',
    );
    const config = await loadConfig(tempDir);
    expect(config.project).toBe('wrapped');
  });

  it('resolves token from environment', async () => {
    process.env.PBVEX_LOCAL_TOKEN = 'env-token';
    tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-config-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `export default {\n  defaultTarget: 'local',\n  targets: { local: { url: 'http://localhost:8090', metadata: {} } },\n};\n`,
      'utf-8',
    );
    const config = await loadConfig(tempDir);
    expect(config.token).toBe('env-token');
  });

  it('loads default config when no pbvex.config.ts exists', async () => {
    tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-config-'));
    await mkdir(path.join(tempDir, 'pbvex'), { recursive: true });
    await expect(loadConfig(tempDir)).rejects.toThrow('Target "local" not found in pbvex.config.ts');
  });

  it('resolveToken reads from credentials file', async () => {
    tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-config-'));
    const credsDir = path.join(tempDir, '.pbvex');
    await mkdir(credsDir, { recursive: true });
    await writeFile(path.join(credsDir, 'credentials.json'), JSON.stringify({ local: { token: 'cred-token' } }, null, 2), 'utf-8');
    const token = await resolveToken(tempDir, 'local');
    expect(token).toBe('cred-token');
  });

  it('rejects a config with side-effect top-level code', async () => {
    tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-config-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `console.log('side effect');\nexport default { defaultTarget: 'local', targets: { local: { url: 'http://localhost:8090', metadata: {} } } };\n`,
      'utf-8',
    );
    await expect(loadConfig(tempDir)).rejects.toThrow('not allowed');
  });

  it('rejects a config with infinite loops without executing them', async () => {
    tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-config-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `while (true) {}\nexport default { defaultTarget: 'local', targets: { local: { url: 'http://localhost:8090', metadata: {} } } };\n`,
      'utf-8',
    );
    await expect(loadConfig(tempDir)).rejects.toThrow('not allowed');
  });

  it('rejects a config that calls arbitrary functions', async () => {
    tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-config-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `export default { defaultTarget: 'local', targets: { local: { url: require('node:path').join('x', 'y'), metadata: {} } } };\n`,
      'utf-8',
    );
    await expect(loadConfig(tempDir)).rejects.toThrow();
  });

  it('rejects a config with external identifiers', async () => {
    tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-config-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `const url = 'http://localhost:8090';\nexport default { defaultTarget: 'local', targets: { local: { url, metadata: {} } } };\n`,
      'utf-8',
    );
    await expect(loadConfig(tempDir)).rejects.toThrow('not allowed');
  });

  it('rejects a config with member access in values', async () => {
    tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-config-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `export default { defaultTarget: 'local', targets: { local: { url: process.env.URL, metadata: {} } } };\n`,
      'utf-8',
    );
    await expect(loadConfig(tempDir)).rejects.toThrow();
  });
});
