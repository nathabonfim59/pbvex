import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { spawnServer } from '@pbvex/server';

const migrationTemplate = `/// <reference path="../_generated/pocketbase.d.ts" />

migrate((app) => {
  // Apply the migration.
}, (app) => {
  // Revert the migration.
});
`;

const migrationTsconfig = `${JSON.stringify({
  compilerOptions: {
    allowJs: true,
    checkJs: true,
    noEmit: true,
    strict: true,
    target: 'ES2022',
    module: 'ESNext',
    moduleResolution: 'bundler',
    skipLibCheck: true,
    types: [],
  },
  include: ['*.js'],
}, null, 2)}\n`;

export interface PocketBaseMigrationScaffoldOptions {
  rootDir: string;
  name: string;
  pocketBaseTypes: string;
  now?: Date;
}

export interface PocketBaseMigrationScaffoldResult {
  migrationPath: string;
  typesPath: string;
  tsconfigPath: string;
}

export function migrationSlug(name: string): string {
  const slug = name
    .trim()
    .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
    .replace(/[^A-Za-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
    .toLowerCase();
  if (slug.length === 0) throw new Error('Migration name must contain at least one letter or number');
  if (slug.length > 100) throw new Error('Migration name must be at most 100 characters after normalization');
  return slug;
}

export async function createPocketBaseMigrationScaffold(options: PocketBaseMigrationScaffoldOptions): Promise<PocketBaseMigrationScaffoldResult> {
  const migrationsDir = path.join(options.rootDir, 'pbvex', 'pocketbaseMigrations');
  const generatedDir = path.join(options.rootDir, 'pbvex', '_generated');
  const timestamp = Math.floor((options.now ?? new Date()).getTime() / 1000);
  const migrationPath = path.join(migrationsDir, `${timestamp}_${migrationSlug(options.name)}.js`);
  const typesPath = path.join(generatedDir, 'pocketbase.d.ts');
  const tsconfigPath = path.join(migrationsDir, 'tsconfig.json');

  if (existsSync(migrationPath)) {
    throw new Error(`Migration already exists: ${path.relative(options.rootDir, migrationPath)}`);
  }

  await mkdir(migrationsDir, { recursive: true });
  await mkdir(generatedDir, { recursive: true });
  await writeFile(typesPath, options.pocketBaseTypes, 'utf8');
  if (!existsSync(tsconfigPath)) await writeFile(tsconfigPath, migrationTsconfig, 'utf8');
  await writeFile(migrationPath, migrationTemplate, { encoding: 'utf8', flag: 'wx' });

  return { migrationPath, typesPath, tsconfigPath };
}

export async function generatePocketBaseTypes(): Promise<string> {
  const temporaryRoot = await mkdtemp(path.join(tmpdir(), 'pbvex-migration-types-'));
  const dataDir = path.join(temporaryRoot, 'data');
  const migrationsDir = path.join(temporaryRoot, 'migrations');

  try {
    const child = spawnServer([
      '--dir', dataDir,
      '--migrationsDir', migrationsDir,
      'migrate', 'history-sync',
    ], { stdio: ['ignore', 'pipe', 'pipe'] });
    let output = '';
    child.stdout?.on('data', (chunk) => { output += String(chunk); });
    child.stderr?.on('data', (chunk) => { output += String(chunk); });

    await new Promise<void>((resolve, reject) => {
      child.once('error', reject);
      child.once('exit', (code, signal) => {
        if (code === 0) {
          resolve();
          return;
        }
        reject(new Error(
          `PocketBase type generation failed (${signal ? `signal ${signal}` : `exit ${code ?? 'unknown'}`})${output.trim() ? `: ${output.trim()}` : ''}`,
        ));
      });
    });

    return await readFile(path.join(dataDir, 'types.d.ts'), 'utf8');
  } finally {
    await rm(temporaryRoot, { recursive: true, force: true });
  }
}
