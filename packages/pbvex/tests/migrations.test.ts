import { afterEach, describe, expect, it } from 'vitest';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { createPocketBaseMigrationScaffold, migrationSlug } from '../src/migrations/pocketbaseScaffold.js';
import { createSchemaMigrationScaffold } from '../src/migrations/schemaScaffold.js';

describe('migration scaffolding', () => {
  let rootDir: string | undefined;

  afterEach(async () => {
    if (rootDir) await rm(rootDir, { recursive: true, force: true });
    rootDir = undefined;
  });

  it('normalizes safe migration names', () => {
    expect(migrationSlug('Create Users Auth')).toBe('create_users_auth');
    expect(migrationSlug('addOAuthProvider')).toBe('add_oauth_provider');
    expect(() => migrationSlug('../../')).toThrow(/letter or number/);
  });

  it('writes a typed migration without replacing an existing scaffold', async () => {
    rootDir = await mkdtemp(path.join(tmpdir(), 'pbvex-migration-'));
    const options = {
      rootDir,
      name: 'Create Users',
      pocketBaseTypes: 'declare function migrate(up: (app: unknown) => void, down: (app: unknown) => void): void;\n',
      now: new Date('2026-07-18T12:00:00Z'),
    };

    const result = await createPocketBaseMigrationScaffold(options);
    expect(path.basename(result.migrationPath)).toBe('1784376000_create_users.js');
    expect(await readFile(result.migrationPath, 'utf8')).toContain('../_generated/pocketbase.d.ts');
    expect(await readFile(result.typesPath, 'utf8')).toContain('declare function migrate');
    expect(JSON.parse(await readFile(result.tsconfigPath, 'utf8'))).toMatchObject({
      compilerOptions: { allowJs: true, checkJs: true, strict: true },
      include: ['*.js'],
    });
    await expect(createPocketBaseMigrationScaffold(options)).rejects.toThrow(/already exists/);
  });

  it('writes a typed PBVex migration from table descriptors', async () => {
    rootDir = await mkdtemp(path.join(tmpdir(), 'pbvex-schema-migration-'));
    const result = await createSchemaMigrationScaffold({
      rootDir,
      name: 'Add Active',
      table: 'users',
      now: new Date('2026-07-18T12:00:00Z'),
      sourceTable: { tableName: 'users', fields: { name: { type: 'string' } } },
      targetTable: { tableName: 'users', fields: { name: { type: 'string' }, active: { type: 'boolean' } } },
    });
    expect(path.basename(result.migrationPath)).toBe('1784376000_add_active.ts');
    const source = await readFile(result.migrationPath, 'utf8');
    expect(source).toContain("import { defineMigration } from 'pbvex/server'");
    expect(source).toContain("table: \"users\"");
    expect(source).toContain('active\": v.boolean()');
  });
});
