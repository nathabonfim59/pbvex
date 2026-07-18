import { afterEach, describe, expect, it } from 'vitest';
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const cliPath = path.resolve(fileURLToPath(import.meta.url), '../../dist/cli/index.js');
const typescriptCli = path.resolve(fileURLToPath(import.meta.url), '../../node_modules/typescript/bin/tsc');

function runCli(cwd: string, ...args: string[]): string {
  return execFileSync(process.execPath, [cliPath, ...args], {
    cwd,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  });
}

describe('pbvex init', () => {
  let projectDir: string | undefined;

  afterEach(async () => {
    if (projectDir) await rm(projectDir, { recursive: true, force: true });
    projectDir = undefined;
  });

  it('creates a usable project that codegens and builds', async () => {
    projectDir = await mkdtemp(path.join(tmpdir(), 'pbvex-init-'));

    expect(runCli(projectDir, 'init')).toContain('Initialized PBVex project');

    const messages = await readFile(path.join(projectDir, 'pbvex', 'messages.ts'), 'utf8');
    expect(messages).toContain("ctx.db.insert('messages'");
    expect(messages).not.toContain('placeholder');

    expect(runCli(projectDir, 'codegen')).toContain('Generated pbvex/_generated files');
    expect(runCli(projectDir, 'build')).toContain('Wrote artifact');
    expect(runCli(projectDir, 'migrations', 'pocketbase', 'create', 'create users')).toContain('Created pbvex/pocketbaseMigrations/');

    const migrationsDir = path.join(projectDir, 'pbvex', 'pocketbaseMigrations');
    const migrationFiles = (await readdir(migrationsDir)).filter((file) => file.endsWith('.js'));
    expect(migrationFiles).toHaveLength(1);
    expect(migrationFiles[0]).toMatch(/^\d+_create_users\.js$/);
    expect(await readFile(path.join(migrationsDir, migrationFiles[0]), 'utf8')).toContain(
      '../_generated/pocketbase.d.ts',
    );
    expect(await readFile(path.join(projectDir, 'pbvex', '_generated', 'pocketbase.d.ts'), 'utf8')).toContain(
      'declare function migrate',
    );
    expect(() => execFileSync(process.execPath, [
      typescriptCli,
      '--noEmit',
      '--project',
      path.join(migrationsDir, 'tsconfig.json'),
    ], { cwd: projectDir, stdio: 'pipe' })).not.toThrow();

    const artifact = JSON.parse(await readFile(path.join(projectDir, '.pbvex', 'dist', 'artifact.json'), 'utf8'));
    expect(artifact.manifest.functions).toHaveLength(2);
    expect(artifact.manifest.functions.map((fn: { exportName: string }) => fn.exportName).sort()).toEqual(['get', 'send']);
    expect(artifact.manifest.schema.tables.map((table: { tableName: string }) => table.tableName).sort()).toEqual(['messages']);

    const packageManifest = JSON.parse(await readFile(path.join(projectDir, 'package.json'), 'utf8'));
    const pbvexManifest = JSON.parse(await readFile(path.resolve(fileURLToPath(import.meta.url), '../../package.json'), 'utf8'));
    expect(packageManifest.devDependencies.pbvex).toBe(`^${pbvexManifest.version}`);
    expect(packageManifest.devDependencies.typescript).toBeUndefined();
    expect(packageManifest.scripts).toMatchObject({
      'pbvex:dev': 'pbvex dev',
      'pbvex:serve': 'pbvex serve',
      'pbvex:deploy': 'pbvex deploy',
      'pbvex:typecheck': 'pbvex typecheck',
    });
    expect(runCli(projectDir, '--version').trim()).toBe(pbvexManifest.version);
  });

  it('initializes an installed-package project without replacing project configuration', async () => {
    projectDir = await mkdtemp(path.join(tmpdir(), 'pbvex-init-installed-'));
    const originalPackage = {
      name: 'existing-app',
      private: true,
      scripts: { dev: 'vite', test: 'vitest' },
      dependencies: { react: '^19.0.0' },
      devDependencies: { pbvex: '^0.1.0', typescript: '^5.7.0' },
    };
    await writeFile(path.join(projectDir, 'package.json'), JSON.stringify(originalPackage, null, 2) + '\n');
    const tsconfig = `{
  // Existing JSONC configuration must remain byte-for-byte intact.
  "extends": "./tsconfig.base.json"
}\n`;
    await writeFile(path.join(projectDir, 'tsconfig.json'), tsconfig);
    await writeFile(path.join(projectDir, '.gitignore'), 'dist\n');

    expect(runCli(projectDir, 'init')).toContain('Initialized PBVex project');

    const updatedPackage = JSON.parse(await readFile(path.join(projectDir, 'package.json'), 'utf8'));
    expect(updatedPackage.name).toBe('existing-app');
    expect(updatedPackage.scripts).toMatchObject({
      dev: 'vite',
      test: 'vitest',
      build: 'pbvex build',
      typecheck: 'pbvex typecheck',
      'pbvex:dev': 'pbvex dev',
      'pbvex:serve': 'pbvex serve',
    });
    expect(updatedPackage.dependencies).toEqual({ react: '^19.0.0' });
    expect(updatedPackage.devDependencies).toEqual(originalPackage.devDependencies);
    expect(await readFile(path.join(projectDir, 'tsconfig.json'), 'utf8')).toBe(tsconfig);
    expect(await readFile(path.join(projectDir, '.gitignore'), 'utf8')).toBe(
      'dist\nnode_modules\n.pbvex/credentials.json\n.pbvex/dist\n.pbvex/dev\n',
    );
    expect(existsSync(path.join(projectDir, 'pbvex', 'messages.ts'))).toBe(true);
  });

  it('allows package scripts to be declined explicitly', async () => {
    projectDir = await mkdtemp(path.join(tmpdir(), 'pbvex-init-no-scripts-'));
    runCli(projectDir, 'init', '--no-scripts');
    const manifest = JSON.parse(await readFile(path.join(projectDir, 'package.json'), 'utf8'));
    expect(manifest.scripts).toEqual({});
    expect(manifest.devDependencies.pbvex).toBeDefined();
  });

  it('preflights conflicts and leaves no partial scaffold', async () => {
    projectDir = await mkdtemp(path.join(tmpdir(), 'pbvex-init-conflict-'));
    const pbvexDir = path.join(projectDir, 'pbvex');
    await mkdir(pbvexDir);
    await writeFile(path.join(pbvexDir, 'existing.ts'), 'keep me\n');

    expect(() => runCli(projectDir!, 'init')).toThrow(/Refusing to overwrite/);

    expect(await readFile(path.join(pbvexDir, 'existing.ts'), 'utf8')).toBe('keep me\n');
    for (const file of ['package.json', 'tsconfig.json', '.gitignore']) {
      expect(existsSync(path.join(projectDir, file))).toBe(false);
    }
    for (const file of ['pbvex.config.ts', 'schema.ts', 'messages.ts']) {
      expect(existsSync(path.join(pbvexDir, file))).toBe(false);
    }
  });

  it('rejects malformed package metadata before writing any scaffold files', async () => {
    projectDir = await mkdtemp(path.join(tmpdir(), 'pbvex-init-invalid-package-'));
    await writeFile(path.join(projectDir, 'package.json'), '{ invalid json\n');

    expect(() => runCli(projectDir!, 'init')).toThrow(/Cannot update package\.json/);
    expect(existsSync(path.join(projectDir, 'pbvex'))).toBe(false);
    expect(existsSync(path.join(projectDir, 'tsconfig.json'))).toBe(false);
    expect(existsSync(path.join(projectDir, '.gitignore'))).toBe(false);
  });

  it('requires --force before replacing managed files', async () => {
    projectDir = await mkdtemp(path.join(tmpdir(), 'pbvex-init-force-'));
    runCli(projectDir, 'init');
    const messagesPath = path.join(projectDir, 'pbvex', 'messages.ts');
    await writeFile(messagesPath, 'custom contents\n');

    expect(() => runCli(projectDir!, 'init')).toThrow(/--force/);
    expect(await readFile(messagesPath, 'utf8')).toBe('custom contents\n');

    runCli(projectDir, 'init', '--force');
    expect(await readFile(messagesPath, 'utf8')).toContain("ctx.db.insert('messages'");
  });
});
