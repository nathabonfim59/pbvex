#!/usr/bin/env node
import { Command } from 'commander';
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { createInterface } from 'node:readline/promises';
import { spawnServer } from '@pbvex/server';
import { loadConfig } from '../config/config.js';
import { bundle } from '../bundler/bundler.js';
import { artifactToJson, buildMetadataToJson } from '../bundler/manifest.js';
import { generateCodegenFiles } from '../codegen/codegen.js';
import { DeployClient } from '../deploy/client.js';
import { watchPbvex } from '../watch/watch.js';
import { applicationPocketBaseMigrationsDir, shouldManageBackend, startManagedBackend } from '../dev/backend.js';
import { createPocketBaseMigrationScaffold, generatePocketBaseTypes } from '../migrations/pocketbaseScaffold.js';
import { createSchemaMigrationScaffold } from '../migrations/schemaScaffold.js';
import { activeDeployment, formatMigrationPlan, readActiveArtifact } from '../migrations/plan.js';

const program = new Command();
const require = createRequire(import.meta.url);
const typescriptCli = require.resolve('typescript/bin/tsc');

const packageManifest = JSON.parse(readFileSync(new URL('../../package.json', import.meta.url), 'utf8')) as {
  version?: unknown;
};
if (typeof packageManifest.version !== 'string' || packageManifest.version.length === 0) {
  throw new Error('PBVex package metadata is missing a version');
}
const packageVersion = packageManifest.version;

program.name('pbvex').description('PBVex CLI').version(packageVersion);

type JsonObject = Record<string, unknown>;

function isJsonObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function readProjectPackage(file: string): JsonObject {
  let parsed: unknown;
  try {
    parsed = JSON.parse(readFileSync(file, 'utf8'));
  } catch (error) {
    throw new Error(`Cannot update package.json: ${error instanceof Error ? error.message : String(error)}`);
  }
  if (!isJsonObject(parsed)) throw new Error('Cannot update package.json: expected a JSON object');
  return parsed;
}

function hasPackageDependency(manifest: JsonObject, name: string): boolean {
  return ['dependencies', 'devDependencies', 'peerDependencies', 'optionalDependencies'].some((section) => {
    const dependencies = manifest[section];
    return isJsonObject(dependencies) && typeof dependencies[name] === 'string';
  });
}

function mergedPackageJson(file: string, addScripts = true): string {
  const manifest: JsonObject = existsSync(file)
    ? readProjectPackage(file)
    : { name: 'my-pbvex-project', private: true, version: '0.1.0', type: 'module' };
  const scripts = isJsonObject(manifest.scripts) ? { ...manifest.scripts } : {};
  if (addScripts) {
    scripts.dev ??= 'pbvex dev';
    scripts.build ??= 'pbvex build';
    scripts.typecheck ??= 'pbvex typecheck';
    scripts['pbvex:dev'] ??= 'pbvex dev';
    scripts['pbvex:serve'] ??= 'pbvex serve';
    scripts['pbvex:deploy'] ??= 'pbvex deploy';
    scripts['pbvex:typecheck'] ??= 'pbvex typecheck';
  }
  manifest.scripts = scripts;

  const devDependencies = isJsonObject(manifest.devDependencies) ? { ...manifest.devDependencies } : {};
  if (!hasPackageDependency(manifest, 'pbvex')) devDependencies.pbvex = `^${packageVersion}`;
  manifest.devDependencies = devDependencies;
  return JSON.stringify(manifest, null, 2) + '\n';
}

function mergedGitignore(file: string): string {
  const entries = ['node_modules', '.pbvex/credentials.json', '.pbvex/dist', '.pbvex/dev'];
  const original = existsSync(file) ? readFileSync(file, 'utf8') : '';
  const lines = original.split(/\r?\n/);
  const missing = entries.filter((entry) => !lines.includes(entry));
  if (missing.length === 0) return original;
  const prefix = original.length > 0 && !original.endsWith('\n') ? `${original}\n` : original;
  return `${prefix}${missing.join('\n')}\n`;
}

async function loadCliConfig(cmd: { target?: string; url?: string; token?: string }) {
  return loadConfig(process.cwd(), { target: cmd.target, url: cmd.url, token: cmd.token });
}

async function generateCodegen(config: Awaited<ReturnType<typeof loadConfig>>, result: Awaited<ReturnType<typeof bundle>>) {
  await generateCodegenFiles(
    { rootDir: config.rootDir, functions: result.functions, project: config.project, emailTemplateNames: result.artifact.manifest.emailTemplates?.entries.map((entry) => entry.name) },
    result.schema,
  );
}

program
  .command('init')
  .description('Initialize a new PBVex project in the current directory')
  .option('--force', 'Replace existing PBVex scaffold files')
  .option('--no-scripts', 'Do not add PBVex package scripts')
  .action(async (options: { force?: boolean; scripts?: boolean }, command: Command) => {
    const cwd = process.cwd();
    const pbvexDir = path.join(cwd, 'pbvex');
    const generatedDir = path.join(pbvexDir, '_generated');
    const packageJsonPath = path.join(cwd, 'package.json');
    const scaffoldFiles = new Map<string, string>([
      [
        path.join(pbvexDir, 'pbvex.config.ts'),
        `export default {
  project: 'my-pbvex-project',
  defaultTarget: 'local',
  targets: {
    local: { url: 'http://127.0.0.1:8090', metadata: {} },
    staging: { url: 'https://staging.example.com', metadata: {} },
    production: { url: 'https://example.com', metadata: {} },
  },
};
`,
      ],
      [
        path.join(pbvexDir, 'schema.ts'),
        `import { defineSchema, defineTable } from 'pbvex/server';
import { v } from 'pbvex/values';

export default defineSchema({
  messages: defineTable({
    body: v.string(),
    channel: v.optional(v.string()),
  }),
});
`,
      ],
      [
        path.join(pbvexDir, 'messages.ts'),
        `import { query, mutation } from 'pbvex/server';
import { v } from 'pbvex/values';

export const get = query({
  args: { channel: v.string() },
  returns: v.array(v.string()),
  handler: async (ctx, args) => {
    return [];
  },
});

export const send = mutation({
  args: { channel: v.string(), body: v.string() },
  returns: v.id('messages'),
  handler: async (ctx, args) => {
    return ctx.db.insert('messages', {
      channel: args.channel,
      body: args.body,
    });
  },
});
`,
      ],
      [path.join(generatedDir, 'api.ts'), `// Generated by PBVex - do not edit manually\n`],
      [path.join(generatedDir, 'dataModel.ts'), `// Generated by PBVex - do not edit manually\n`],
      [path.join(generatedDir, 'server.ts'), `// Generated by PBVex - do not edit manually\n`],
    ]);

    const tsconfigPath = path.join(cwd, 'tsconfig.json');
    const gitignorePath = path.join(cwd, '.gitignore');
    let addScripts = options.scripts !== false;
    if (command.getOptionValueSource('scripts') === 'default' && process.stdin.isTTY && process.stdout.isTTY) {
      const prompt = createInterface({ input: process.stdin, output: process.stdout });
      const answer = (await prompt.question('Add PBVex package scripts? (Y/n) ')).trim().toLowerCase();
      prompt.close();
      addScripts = answer === '' || answer === 'y' || answer === 'yes';
    }

    const projectFiles = new Map<string, string>([
      [packageJsonPath, mergedPackageJson(packageJsonPath, addScripts)],
      [
        tsconfigPath,
        existsSync(tsconfigPath)
          ? readFileSync(tsconfigPath, 'utf8')
          : JSON.stringify(
              {
                compilerOptions: {
                  target: 'ES2022',
                  module: 'ESNext',
                  moduleResolution: 'bundler',
                  lib: ['ES2022'],
                  strict: true,
                  esModuleInterop: true,
                  skipLibCheck: true,
                  forceConsistentCasingInFileNames: true,
                  resolveJsonModule: true,
                  allowSyntheticDefaultImports: true,
                },
                include: ['pbvex/**/*.ts'],
                exclude: ['node_modules'],
              },
              null,
              2,
            ) + '\n',
      ],
      [gitignorePath, mergedGitignore(gitignorePath)],
    ]);

    if (!options.force) {
      const conflicts = [...scaffoldFiles.keys()].filter(existsSync);
      if (existsSync(pbvexDir) && readdirSync(pbvexDir).length > 0 && !conflicts.some((file) => file.startsWith(pbvexDir + path.sep))) {
        conflicts.push(pbvexDir);
      }
      if (conflicts.length > 0) {
        const relative = conflicts.map((file) => path.relative(cwd, file) || '.').sort();
        throw new Error(`Refusing to overwrite existing PBVex project paths:\n${relative.map((file) => `  - ${file}`).join('\n')}\nRe-run with --force to replace them.`);
      }
    }

    // All conflict checks happen before the first filesystem mutation.
    mkdirSync(generatedDir, { recursive: true });
    for (const [file, contents] of [...projectFiles, ...scaffoldFiles]) {
      mkdirSync(path.dirname(file), { recursive: true });
      writeFileSync(file, contents, 'utf-8');
    }

    console.log('Initialized PBVex project in ./pbvex');
  });

program
  .command('codegen')
  .description('Generate pbvex/_generated files')
  .option('-t, --target <target>', 'Target name', 'local')
  .option('--url <url>', 'Override target URL')
  .option('--token <token>', 'API token')
  .action(async (options) => {
    const config = await loadCliConfig(options);
    const result = await bundle({ rootDir: config.rootDir, project: config.project, target: config.target });
    if (result.diagnostics.length > 0) {
      console.error(result.diagnostics.join('\n'));
      process.exit(1);
    }
    await generateCodegen(config, result);
    console.log('Generated pbvex/_generated files');
  });

const migrationsCommand = program
  .command('migrations')
  .description('Create and plan PBVex schema migrations');

migrationsCommand
  .command('create')
  .description('Create a typed PBVex migration in pbvex/migrations')
  .argument('<name>', 'Migration name')
  .requiredOption('--table <table>', 'PBVex table to migrate')
  .option('-t, --target <target>', 'Target name', 'local')
  .option('--url <url>', 'Override target URL')
  .option('--token <token>', 'API token')
  .option('--active-artifact <path>', 'Validated deployment artifact to use as the offline source')
  .action(async (name: string, options: { table: string; target?: string; url?: string; token?: string; activeArtifact?: string }) => {
    const config = await loadCliConfig(options);
    const candidate = await bundle({ rootDir: config.rootDir, project: config.project, target: config.target });
    if (candidate.diagnostics.length > 0) throw new Error(candidate.diagnostics.join('\n'));
    const targetTable = candidate.artifact.manifest.schema?.tables.find((table) => table.tableName === options.table);
    if (!targetTable) throw new Error(`Table ${JSON.stringify(options.table)} is not present in the local schema`);
    let source = options.activeArtifact
      ? await readActiveArtifact(path.resolve(config.rootDir, options.activeArtifact))
      : undefined;
    if (!source) {
      try { source = activeDeployment((await new DeployClient({ url: config.url, token: config.token }).list()).deployments); }
      catch { /* Scaffolding remains usable when the selected target is offline. */ }
    }
    const sourceTable = source?.manifest.schema?.tables.find((table) => table.tableName === options.table);
    const result = await createSchemaMigrationScaffold({
      rootDir: config.rootDir,
      name,
      table: options.table,
      sourceTable,
      targetTable,
    });
    console.log(`Created ${path.relative(config.rootDir, result.migrationPath)}`);
    if (result.usedEmptySource) console.log('No active source table found; review the scaffolded from validator before deploying.');
  });

migrationsCommand
  .command('plan')
  .description('Compare the local candidate with the active source schema')
  .option('-t, --target <target>', 'Target name', 'local')
  .option('--url <url>', 'Override target URL')
  .option('--token <token>', 'API token')
  .option('--active-artifact <path>', 'Validated deployment artifact to use as the offline source')
  .action(async (options: { target?: string; url?: string; token?: string; activeArtifact?: string }) => {
    const config = await loadCliConfig(options);
    const candidate = await bundle({ rootDir: config.rootDir, project: config.project, target: config.target });
    if (candidate.diagnostics.length > 0) throw new Error(candidate.diagnostics.join('\n'));
    const source = options.activeArtifact
      ? await readActiveArtifact(path.resolve(config.rootDir, options.activeArtifact))
      : activeDeployment((await new DeployClient({ url: config.url, token: config.token }).list()).deployments);
    console.log(await formatMigrationPlan(candidate.artifact, source));
  });

const pocketBaseMigrationsCommand = migrationsCommand
  .command('pocketbase')
  .description('Manage direct PocketBase host migrations');

pocketBaseMigrationsCommand
  .command('create')
  .description('Create a typed PocketBase migration in pbvex/pocketbaseMigrations')
  .argument('<name>', 'Migration name')
  .action(async (name: string) => {
    const rootDir = process.cwd();
    const pocketBaseTypes = await generatePocketBaseTypes();
    const result = await createPocketBaseMigrationScaffold({ rootDir, name, pocketBaseTypes });
    console.log(`Created ${path.relative(rootDir, result.migrationPath)}`);
    console.log(`Generated ${path.relative(rootDir, result.typesPath)}`);
  });

program
  .command('typecheck')
  .description('Run TypeScript typecheck on the project')
  .option('-t, --target <target>', 'Target name', 'local')
  .option('--url <url>', 'Override target URL')
  .option('--token <token>', 'API token')
  .action(async (options) => {
    const config = await loadCliConfig(options);
    const result = await bundle({ rootDir: config.rootDir, project: config.project, target: config.target });
    if (result.diagnostics.length > 0) {
      console.error(result.diagnostics.join('\n'));
      process.exit(1);
    }
    await generateCodegen(config, result);
    try {
      execFileSync(process.execPath, [typescriptCli, '--noEmit'], { cwd: config.rootDir, stdio: 'inherit' });
      const migrationsTsconfig = path.join(config.rootDir, 'pbvex', 'pocketbaseMigrations', 'tsconfig.json');
      if (existsSync(migrationsTsconfig)) {
        execFileSync(process.execPath, [typescriptCli, '--noEmit', '--project', migrationsTsconfig], {
          cwd: config.rootDir,
          stdio: 'inherit',
        });
      }
    } catch {
      process.exit(1);
    }
  });

program
  .command('build')
  .description('Bundle the project into a deployment artifact')
  .option('--check', 'Validate without writing artifact')
  .option('-t, --target <target>', 'Target name', 'local')
  .option('--url <url>', 'Override target URL')
  .option('--token <token>', 'API token')
  .action(async (options) => {
    const config = await loadCliConfig(options);
    const result = await bundle({ rootDir: config.rootDir, project: config.project, target: config.target });
    if (result.diagnostics.length > 0) {
      console.error(result.diagnostics.join('\n'));
      process.exit(1);
    }
    await generateCodegen(config, result);
    if (!options.check) {
      const distDir = path.join(config.rootDir, '.pbvex', 'dist');
      mkdirSync(distDir, { recursive: true });
      const artifactPath = path.join(distDir, 'artifact.json');
      const metadataPath = path.join(distDir, 'build-metadata.json');
      writeFileSync(artifactPath, artifactToJson(result.artifact), 'utf-8');
      writeFileSync(
        metadataPath,
        buildMetadataToJson({
          project: config.project,
          target: config.target,
          modules: result.artifact.modules,
          diagnostics: result.diagnostics,
        }),
        'utf-8',
      );
      console.log(`Wrote artifact to ${artifactPath}`);
      console.log(`Wrote build metadata to ${metadataPath}`);
    } else {
      console.log('Build validation passed');
    }
  });

program
  .command('serve')
  .description('Run the bundled PBVex backend server')
  .helpOption(false)
  .allowUnknownOption(true)
  .option('--pocketbaseMigrationsDir <path>', 'Override the pbvex/pocketbaseMigrations directory')
  .argument('[serverArgs...]')
  .action(async (serverArgs: string[], options: { pocketbaseMigrationsDir?: string }) => {
    if (serverArgs.some((arg) => arg === '--migrationsDir' || arg.startsWith('--migrationsDir='))) {
      throw new Error('pbvex serve uses --pocketbaseMigrationsDir; --migrationsDir is only a direct backend flag');
    }
    const args = [
      '--migrationsDir',
      applicationPocketBaseMigrationsDir(process.cwd(), options.pocketbaseMigrationsDir),
      'serve',
      ...serverArgs,
    ];
    const child = spawnServer(args, { stdio: 'inherit' });
    const exitCode = await new Promise<number>((resolve, reject) => {
      child.once('error', reject);
      child.once('exit', (code, signal) => {
        if (signal) {
          resolve(1);
          return;
        }
        resolve(code ?? 1);
      });
    });
    if (exitCode !== 0) process.exitCode = exitCode;
  });

program
  .command('dev')
  .description('Watch pbvex files and deploy to the selected target')
  .option('-t, --target <target>', 'Target name', 'local')
  .option('--url <url>', 'Override target URL')
  .option('--token <token>', 'API token')
  .option('--no-backend', 'Do not start a managed backend for a loopback local target')
  .option('--no-admin-ui', 'Disable the PocketBase admin dashboard for the managed backend')
  .option('--debug', 'Enable verbose PocketBase and SQL logging for the managed backend')
  .option('--pocketbaseMigrationsDir <path>', 'Override the pbvex/pocketbaseMigrations directory for the managed backend')
  .action(async (options: { target?: string; url?: string; token?: string; backend?: boolean; adminUi?: boolean; debug?: boolean; pocketbaseMigrationsDir?: string }) => {
    let config = await loadCliConfig(options);
    const managedBackend = options.backend !== false && shouldManageBackend(config)
      ? await startManagedBackend(config, {
          debug: options.debug,
          adminUI: options.adminUi,
          pocketbaseMigrationsDir: options.pocketbaseMigrationsDir,
        })
      : undefined;
    if (managedBackend) config = { ...config, token: managedBackend.token };
    const build = () => bundle({ rootDir: config.rootDir, project: config.project, target: config.target });

    const runBuild = async () => {
      const result = await build();
      if (result.diagnostics.length > 0) {
        console.error(result.diagnostics.join('\n'));
        return;
      }
      await generateCodegen(config, result);
      const client = new DeployClient({ url: config.url, token: config.token });
      const deployResult = await client.deploy(result.artifact);
      if (deployResult.ok) {
        console.log(`Deployed ${deployResult.deploymentId} to ${config.url}`);
      } else {
        console.error(`Deploy failed: ${deployResult.error}`);
      }
    };

    await runBuild();

    const { ready, close } = watchPbvex({
      config,
      build,
      generateCodegen: (result) => generateCodegen(config, result),
      deploy: async (result) => {
        const client = new DeployClient({ url: config.url, token: config.token });
        const deployResult = await client.deploy(result.artifact);
        if (deployResult.ok) {
          console.log(`Deployed ${deployResult.deploymentId} to ${config.url}`);
        } else {
          throw new Error(`Deploy failed: ${deployResult.error}`);
        }
      },
      onChange: ({ ok, diagnostics, error }) => {
        if (diagnostics.length > 0) console.error(diagnostics.join('\n'));
        if (error) console.error(error);
        if (ok) console.log('Build and deploy succeeded');
      },
      debounceMs: 300,
    });
    await ready;

    const shutdown = async () => {
      await close();
      await managedBackend?.close();
      process.exit(0);
    };
    process.once('SIGINT', shutdown);
    process.once('SIGTERM', shutdown);
  });

program
  .command('deploy')
  .description('Bundle and deploy to the selected target')
  .option('-t, --target <target>', 'Target name', 'local')
  .option('--url <url>', 'Override target URL')
  .option('--token <token>', 'API token')
  .action(async (options) => {
    const config = await loadCliConfig(options);
    const result = await bundle({ rootDir: config.rootDir, project: config.project, target: config.target });
    if (result.diagnostics.length > 0) {
      console.error(result.diagnostics.join('\n'));
      process.exit(1);
    }
    await generateCodegen(config, result);
    const client = new DeployClient({ url: config.url, token: config.token });
    const deployResult = await client.deploy(result.artifact);
    if (deployResult.ok) {
      console.log(`Deployed ${deployResult.deploymentId} to ${config.url}`);
    } else {
      console.error(`Deploy failed: ${deployResult.error}`);
      process.exit(1);
    }
  });

await program.parseAsync();
