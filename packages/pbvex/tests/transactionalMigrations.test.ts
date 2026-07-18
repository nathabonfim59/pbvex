import { afterEach, describe, expect, it } from 'vitest';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { createContext, runInNewContext } from 'node:vm';
import { validateManifest } from '@pbvex/protocol';
import { bundle, discoverMigrationModules, discoverModules } from '../src/bundler/bundler.js';
import { canonicalSchemaHash } from '../src/bundler/manifest.js';
import { canonicalHash } from '@pbvex/protocol';

describe('transactional migrations', () => {
  let rootDir: string | undefined;

  afterEach(async () => {
    if (rootDir) await rm(rootDir, { recursive: true, force: true });
    rootDir = undefined;
  });

  async function makeProject(): Promise<string> {
    rootDir = await mkdtemp(path.join(tmpdir(), 'pbvex-transactional-migration-'));
    await mkdir(path.join(rootDir, 'pbvex', 'migrations'), { recursive: true });
    await mkdir(path.join(rootDir, 'pbvex', 'pocketbaseMigrations'), { recursive: true });
    await writeFile(path.join(rootDir, 'pbvex', 'ordinary.ts'), `
      import { query } from 'pbvex/server';
      export const visible = query({ handler: () => null });
    `);
    await writeFile(path.join(rootDir, 'pbvex', 'schema.ts'), `
      import { defineSchema, defineTable } from 'pbvex/server';
      import { v } from 'pbvex/values';
      export default defineSchema({ users: defineTable({ name: v.string(), active: v.boolean() }) });
    `);
    await writeFile(path.join(rootDir, 'pbvex', 'pocketbaseMigrations', 'legacy.ts'), 'throw new Error("must not evaluate");');
    await writeFile(path.join(rootDir, 'pbvex', 'migrations', 'helper.ts'), `
      export const normalize = (name: string) => name.trim();
    `);
    await writeFile(path.join(rootDir, 'pbvex', 'migrations', 'add_active.ts'), `
      import { defineMigration } from 'pbvex/server';
      import { v } from 'pbvex/values';
      import { normalize } from './helper';
      export const addActive = defineMigration({
        id: '20260718_add_active', table: 'users', mode: 'transactional',
        from: v.object({ name: v.string() }),
        to: v.object({ name: v.string(), active: v.boolean() }),
        up: (doc) => ({ name: normalize(doc.name), active: true }),
        down: (doc) => ({ name: doc.name }),
      });
    `);
    return rootDir;
  }

  it('discovers migrations separately and excludes reserved trees from functions', async () => {
    const root = await makeProject();
    expect(await discoverModules(root)).toEqual(['pbvex/ordinary.ts', 'pbvex/schema.ts']);
    expect(await discoverMigrationModules(root)).toEqual([
      'pbvex/migrations/add_active.ts',
      'pbvex/migrations/helper.ts',
    ]);
  });

  it('emits deterministic descriptors, hashes, checksums, and dedicated registration', async () => {
    const root = await makeProject();
    const first = await bundle({ rootDir: root, project: 'migrations', target: 'local' });
    const second = await bundle({ rootDir: root, project: 'migrations', target: 'local' });
    expect(first.diagnostics).toEqual([]);
    expect(second.diagnostics).toEqual([]);
    expect(first.migrations).toHaveLength(1);
    expect(first.artifact.manifest.migrations).toEqual(second.artifact.manifest.migrations);
    expect(first.artifact.manifest.deploymentId).toBe(second.artifact.manifest.deploymentId);
    const descriptor = first.artifact.manifest.migrations![0]!;
    expect(descriptor).toEqual(expect.objectContaining({
      id: '20260718_add_active', table: 'users', mode: 'transactional',
      modulePath: 'pbvex/migrations/add_active.ts', exportName: 'addActive',
      reversibility: 'reversible',
    }));
    expect(descriptor.sourceSchemaHash).toMatch(/^[0-9a-f]{64}$/);
    expect(descriptor.targetSchemaHash).toMatch(/^[0-9a-f]{64}$/);
    expect(descriptor.checksum).toMatch(/^[0-9a-f]{64}$/);
    expect(validateManifest(first.artifact.manifest).migrations).toEqual([descriptor]);

    const registrations: unknown[][] = [];
    const functionRegistrations: unknown[][] = [];
    const source = Buffer.from(first.artifact.bundle, 'base64').toString('utf8');
    runInNewContext(source, createContext({ console, __pbvex: {
      registerFunction: (...args: unknown[]) => functionRegistrations.push(args),
      registerMigration: (...args: unknown[]) => registrations.push(args),
    } }), { timeout: 1000 });
    expect(registrations).toHaveLength(1);
    expect(registrations[0]![0]).toEqual(descriptor);
    expect(registrations[0]!.slice(1).every((value) => typeof value === 'function')).toBe(true);
    expect(functionRegistrations).toHaveLength(1);

    const oldChecksum = descriptor.checksum;
    await writeFile(path.join(root, 'pbvex', 'migrations', 'helper.ts'), `
      export const normalize = (name: string) => name.toUpperCase();
    `);
    const changed = await bundle({ rootDir: root, project: 'migrations', target: 'local' });
    expect(changed.diagnostics).toEqual([]);
    expect(changed.artifact.manifest.migrations![0]!.checksum).not.toBe(oldChecksum);
    expect(changed.artifact.manifest.deploymentId).not.toBe(first.artifact.manifest.deploymentId);
  });

  it('defines the absent schema hash as canonical empty tables', async () => {
    await expect(canonicalSchemaHash()).resolves.toBe(await canonicalHash({ tables: [] }));
  });

  it('rejects duplicate and invalid migration definitions', async () => {
    const root = await makeProject();
    await writeFile(path.join(root, 'pbvex', 'migrations', 'duplicate.ts'), `
      import { defineMigration } from 'pbvex/server'; import { v } from 'pbvex/values';
      export default defineMigration({ id: '20260718_add_active', table: 'users', mode: 'transactional',
        from: v.object({}), to: v.object({}), up: () => ({}), down: () => ({}) });
    `);
    const duplicate = await bundle({ rootDir: root, project: 'migrations', target: 'local' });
    expect(duplicate.diagnostics).toEqual([expect.stringMatching(/Duplicate migration id/)]);

    await rm(path.join(root, 'pbvex', 'migrations', 'duplicate.ts'));
    await writeFile(path.join(root, 'pbvex', 'migrations', 'invalid.ts'), `
      import { defineMigration } from 'pbvex/server'; import { v } from 'pbvex/values';
      export default defineMigration({ id: '../bad', table: 'users', mode: 'transactional',
        from: v.object({}), to: v.object({}), up: () => ({}), down: () => ({}) });
    `);
    const invalid = await bundle({ rootDir: root, project: 'migrations', target: 'local' });
    expect(invalid.diagnostics).toEqual([expect.stringMatching(/Migration id must match/)]);
  });

  it('rejects non-object migration validators at runtime', async () => {
    const { defineMigration } = await import('../src/runtime/server.js');
    const { v } = await import('../src/runtime/values.js');
    expect(() => defineMigration({
      id: 'bad_from', table: 'users', mode: 'transactional',
      from: v.string(), to: v.object({}), up: () => ({}), down: () => '' as any,
    } as any)).toThrow(/object validators/);
  });
});
