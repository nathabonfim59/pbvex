import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { mkdtemp, rm, mkdir, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { watchPbvex } from '../src/watch/watch.js';
import { bundle } from '../src/bundler/bundler.js';
import { generateCodegenFiles } from '../src/codegen/codegen.js';
import type { ResolvedConfig } from '../src/config/config.js';

describe('watch', () => {
  let tempDir: string;

  beforeEach(async () => {
    tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-watch-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `export default { defaultTarget: 'local', targets: { local: { url: 'http://localhost:8090', metadata: {} } } };\n`,
      'utf-8',
    );
    await writeFile(
      path.join(pbvexDir, 'messages.ts'),
      `import { query } from 'pbvex/server';\nimport { v } from 'pbvex/values';\nexport const get = query({ args: { channel: v.string() }, returns: v.array(v.string()), handler: async () => [] });\n`,
      'utf-8',
    );
  });

  afterEach(async () => {
    await rm(tempDir, { recursive: true, force: true });
  });

  it('rebuilds, runs codegen, and deploys on file change', async () => {
    const config: ResolvedConfig = {
      project: 'test',
      defaultTarget: 'local',
      targets: { local: { url: 'http://localhost:8090', metadata: {} } },
      target: 'local',
      url: 'http://localhost:8090',
      token: undefined,
      rootDir: tempDir,
    };

    let buildCalls = 0;
    let codegenCalls = 0;
    let deployCalls = 0;

    const build = async () => {
      buildCalls++;
      const result = await bundle({ rootDir: tempDir, project: 'test', target: 'local' });
      return result;
    };

    const generateCodegen = async (result: Awaited<ReturnType<typeof build>>) => {
      codegenCalls++;
      await generateCodegenFiles({ rootDir: tempDir, functions: result.functions, project: 'test' }, result.schema);
    };

    const deploy = async () => {
      deployCalls++;
    };

    let lastResult: { ok: boolean; diagnostics: string[]; error?: string } | undefined;
    const { ready, close } = watchPbvex({
      config,
      build,
      generateCodegen,
      deploy,
      onChange: (result) => {
        lastResult = result;
      },
      debounceMs: 50,
    });

    await ready;
    await writeFile(path.join(tempDir, 'pbvex', 'messages.ts'), `import { query } from 'pbvex/server';\nimport { v } from 'pbvex/values';\nexport const get = query({ args: { channel: v.string() }, returns: v.array(v.string()), handler: async () => ['first'] });\n`, 'utf-8');

    await new Promise<void>((resolve) => {
      const interval = setInterval(() => {
        if (lastResult) {
          clearInterval(interval);
          resolve();
        }
      }, 10);
    });
    expect(buildCalls).toBeGreaterThanOrEqual(1);
    expect(codegenCalls).toBeGreaterThanOrEqual(1);
    expect(deployCalls).toBeGreaterThanOrEqual(1);
    expect(lastResult?.ok).toBe(true);

    lastResult = undefined;
    await writeFile(path.join(tempDir, 'pbvex', 'messages.ts'), `import { query } from 'pbvex/server';\nimport { v } from 'pbvex/values';\nexport const get = query({ args: { channel: v.string() }, returns: v.array(v.string()), handler: async () => ['changed'] });\n`, 'utf-8');

    await new Promise<void>((resolve) => {
      const timeout = setTimeout(() => {
        clearInterval(interval);
        resolve();
      }, 500);
      const interval = setInterval(() => {
        if (lastResult) {
          clearTimeout(timeout);
          clearInterval(interval);
          resolve();
        }
      }, 10);
    });
    await close();
    expect(codegenCalls).toBeGreaterThanOrEqual(2);
    expect(deployCalls).toBeGreaterThanOrEqual(2);
  });
});
