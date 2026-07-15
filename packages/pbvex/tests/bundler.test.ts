import { describe, it, expect } from 'vitest';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { createContext, runInNewContext } from 'node:vm';
import { validateUploadRequest } from '@pbvex/protocol';
import { bundle, discoverModules } from '../src/bundler/bundler.js';
import { artifactToJson } from '../src/bundler/manifest.js';
import { deriveFunctionName } from '../src/bundler/functionName.js';

const FIXTURE = new URL('../fixtures/example', import.meta.url).pathname;
const COMPONENT_SCHEMA_FIXTURE = new URL('../fixtures/component-schema', import.meta.url).pathname;
const NESTED_COMPONENT_COLLISION_FIXTURE = new URL('../fixtures/nested-component-collision', import.meta.url).pathname;

describe('bundler', () => {
  it('discovers pbvex modules in sorted order', async () => {
    const modules = await discoverModules(FIXTURE);
    expect(modules).toEqual(['pbvex/helpers.ts', 'pbvex/messages.ts', 'pbvex/schema.ts', 'pbvex/users.ts']);
  });

  it('bundles example fixture and extracts functions and schema', async () => {
    const result = await bundle({ rootDir: FIXTURE, project: 'example', target: 'local' });
    expect(result.diagnostics).toEqual([]);
    expect(result.schema).toBeDefined();
    expect([...result.schema!.tableNames].sort()).toEqual(['messages', 'users']);
    expect(result.functions).toHaveLength(5);
    const names = result.functions.map((f) => `${f.modulePath}#${f.exportName}`).sort();
    expect(names).toEqual(['pbvex/messages.ts#internalStats', 'pbvex/messages.ts#list', 'pbvex/messages.ts#send', 'pbvex/users.ts#get', 'pbvex/users.ts#search'].sort());
  });

  it('produces deterministic artifact output', async () => {
    const first = await bundle({ rootDir: FIXTURE, project: 'example', target: 'local' });
    const second = await bundle({ rootDir: FIXTURE, project: 'example', target: 'local' });
    expect(first.diagnostics).toEqual([]);
    expect(second.diagnostics).toEqual([]);
    expect(artifactToJson(first.artifact)).toBe(artifactToJson(second.artifact));
  });

  it('includes bundle code and sha256 in artifact', async () => {
    const result = await bundle({ rootDir: FIXTURE, project: 'example', target: 'local' });
    expect(result.artifact.bundle.length).toBeGreaterThan(0);
    expect(result.artifact.sha256).toMatch(/^[a-f0-9]{64}$/);
    expect(result.artifact.manifest.schema?.tables).toHaveLength(2);
    expect(result.artifact.manifest.functions).toHaveLength(5);
  });

  it('builds a schema-bearing component evaluated in the bundler VM realm', async () => {
    const result = await bundle({ rootDir: COMPONENT_SCHEMA_FIXTURE, project: 'component-schema', target: 'local' });
    expect(result.diagnostics).toEqual([]);
    expect(result.artifact.manifest.components?.definitions).toHaveLength(1);
    expect(result.artifact.manifest.components?.mounts.map((mount) => mount.name)).toEqual(['counter', 'counter2']);
    expect(result.artifact.manifest.components?.definitions[0]?.schema?.tables[0]?.tableName).toBe('counters');
    expect(result.artifact.manifest.functions.map((fn) => fn.modulePath)).toEqual([
      'pbvex/components/counter/store.ts',
      'pbvex/components/counter2/store.ts',
    ]);
    expect(result.artifact.modules.map((module) => module.path)).toEqual([
      'pbvex/components/counter/store.ts',
      'pbvex/components/counter2/store.ts',
    ]);
    await expect(validateUploadRequest(JSON.parse(artifactToJson(result.artifact)))).resolves.toBeDefined();

    const bundleSource = Buffer.from(result.artifact.bundle, 'base64').toString('utf-8');
    const registered: Array<{ modulePath: string }> = [];
    const context = createContext({
      console,
      __pbvex: {
        registerFunction(descriptor: { modulePath: string }) {
          registered.push(descriptor);
        },
      },
    });
    runInNewContext(bundleSource, context, { timeout: 1000 });
    expect(registered.map((descriptor) => descriptor.modulePath)).toEqual([
      'pbvex/components/counter/store.ts',
      'pbvex/components/counter2/store.ts',
    ]);
  });

  it('rejects a parent module path that enters a descendant mount namespace', async () => {
    const result = await bundle({ rootDir: NESTED_COMPONENT_COLLISION_FIXTURE, project: 'nested-collision', target: 'local' });
    expect(result.diagnostics).toEqual([
      expect.stringMatching(/Function module outer\/child\/store\.ts from component mount outer collides with descendant mount outer\/child/),
    ]);
  });

  it('resolves relative imports and includes module code', async () => {
    const result = await bundle({ rootDir: FIXTURE, project: 'example', target: 'local' });
    const usersModule = result.artifact.modules.find((m) => m.path === 'pbvex/users.ts');
    expect(usersModule).toBeDefined();
    const relativeImport = usersModule!.imports.find((i) => i.specifier === './helpers');
    expect(relativeImport).toBeDefined();
    expect(relativeImport!.resolvedSpecifier).toBe('pbvex/helpers.ts');
  });

  it('emits a Goja-compatible IIFE that registers functions with the host bridge', async () => {
    const result = await bundle({ rootDir: FIXTURE, project: 'example', target: 'local' });
    expect(result.diagnostics).toEqual([]);
    const bundleSource = Buffer.from(result.artifact.bundle, 'base64').toString('utf-8');

    const registered: { descriptor: unknown; handler: unknown }[] = [];
    const context = createContext({
      console,
      __pbvex: {
        registerFunction(descriptor: unknown, handler: unknown) {
          registered.push({ descriptor, handler });
        },
      },
    });

    expect(() => runInNewContext(bundleSource, context, { timeout: 1000 })).not.toThrow();
    const registeredDescriptors = registered.map((r) => r.descriptor);
    const expectedDescriptors = [...result.artifact.manifest.functions].sort((a, b) =>
      `${a.modulePath}:${a.exportName}` < `${b.modulePath}:${b.exportName}` ? -1 : `${a.modulePath}:${a.exportName}` > `${b.modulePath}:${b.exportName}` ? 1 : 0,
    );
    expect(registeredDescriptors).toEqual(expectedDescriptors);
    expect(registered.length).toBe(5);
  });

  it('discovers pbvex/crons.ts and emits sorted recurring jobs', async () => {
    const rootDir = await mkdtemp(path.join(tmpdir(), 'pbvex-crons-'));
    try {
      await mkdir(path.join(rootDir, 'pbvex', '_generated'), { recursive: true });
      await writeFile(path.join(rootDir, 'pbvex', 'tasks.ts'), `
        import { internalMutation } from 'pbvex/server';
        import { v } from 'pbvex/values';
        export const cleanup = internalMutation({ args: { scope: v.string() }, handler: async () => null });
      `);
      const functionName = deriveFunctionName('pbvex/tasks.ts', 'cleanup');
      await writeFile(path.join(rootDir, 'pbvex', '_generated', 'api.ts'), `
        export const internal = { tasks: { cleanup: {
          _path: ${JSON.stringify(functionName)}, _type: 'mutation', _visibility: 'internal'
        } } } as const;
      `);
      await writeFile(path.join(rootDir, 'pbvex', 'crons.ts'), `
        import { cronJobs } from 'pbvex/server';
        import { internal } from './_generated/api';
        const crons = cronJobs();
        crons.cron('nightly-cleanup', '0 2 * * *', internal.tasks.cleanup as any, { scope: 'expired' });
        export default crons;
      `);

      const result = await bundle({ rootDir, project: 'cron-test', target: 'local' });
      expect(result.diagnostics).toEqual([]);
      expect(result.artifact.manifest.cronJobs).toEqual([{
        name: 'nightly-cleanup',
        schedule: '0 2 * * *',
        functionName,
        args: { scope: 'expired' },
      }]);
      await expect(validateUploadRequest(JSON.parse(artifactToJson(result.artifact)))).resolves.toBeDefined();
    } finally {
      await rm(rootDir, { recursive: true, force: true });
    }
  });
});
