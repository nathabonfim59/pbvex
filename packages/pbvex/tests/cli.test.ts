import { describe, it, expect, beforeAll } from 'vitest';
import { mkdtemp, rm, mkdir, writeFile, readFile, readdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { execSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const cliPath = path.resolve(fileURLToPath(import.meta.url), '../../dist/cli/index.js');

describe('cli', () => {
  let tempDir: string;

  beforeAll(async () => {
    tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-cli-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });
    await writeFile(
      path.join(pbvexDir, 'pbvex.config.ts'),
      `export default {\n  project: 'cli-test',\n  defaultTarget: 'local',\n  targets: { local: { url: 'http://localhost:8090', metadata: {} } },\n};\n`,
      'utf-8',
    );
    await writeFile(
      path.join(pbvexDir, 'schema.ts'),
      `import { defineSchema, defineTable } from 'pbvex/server';\nimport { v } from 'pbvex/values';\nexport default defineSchema({\n  messages: defineTable({ body: v.string() }),\n});\n`,
      'utf-8',
    );
    await writeFile(
      path.join(pbvexDir, 'messages.ts'),
      `import { query } from 'pbvex/server';\nimport { v } from 'pbvex/values';\nexport const list = query({\n  args: { channel: v.string() },\n  returns: v.array(v.string()),\n  handler: async () => [],\n});\n`,
      'utf-8',
    );
  });

  it('builds and generates codegen from a real .ts config using the built CLI', async () => {
    execSync(`node ${cliPath} build`, { cwd: tempDir, stdio: 'pipe' });
    const artifactPath = path.join(tempDir, '.pbvex', 'dist', 'artifact.json');
    const metadataPath = path.join(tempDir, '.pbvex', 'dist', 'build-metadata.json');
    const artifactRaw = await readFile(artifactPath, 'utf-8');
    const activeArtifactPath = path.join(tempDir, 'active-artifact.json');
    await writeFile(activeArtifactPath, artifactRaw, 'utf-8');
    const artifact = JSON.parse(artifactRaw);
    expect(artifact.manifest.protocolVersion).toBe('v1');
    expect(artifact.project).toBeUndefined();
    expect(artifact.target).toBeUndefined();
		expect(artifact.modules).toBeUndefined();
    expect(artifact.sha256).toMatch(/^[a-f0-9]{64}$/);
    expect(artifact.bundle.length).toBeGreaterThan(0);
    expect(artifact.manifest.functions).toHaveLength(1);
    expect(artifact.manifest.functions[0].exportName).toBe('list');
    expect(artifact.manifest.schema?.tables).toHaveLength(1);
    expect(artifact.manifest.schema?.tables[0].tableName).toBe('messages');

    const metadataRaw = await readFile(metadataPath, 'utf-8');
    const metadata = JSON.parse(metadataRaw);
    expect(metadata.project).toBe('cli-test');
    expect(metadata.target).toBe('local');
    expect(metadata.modules).toBeDefined();
    expect(metadata.diagnostics).toEqual([]);

    const generatedApi = await readFile(path.join(tempDir, 'pbvex', '_generated', 'api.ts'), 'utf-8');
    expect(generatedApi).toContain("import type { FunctionReference } from 'pbvex/server'");
    expect(generatedApi).toContain('"list": { "_path": "pbvex_messages_list_');
    expect(generatedApi).toContain('"_type": "query"');
    expect(generatedApi).toContain('"_visibility": "public"');

    const generatedDataModel = await readFile(path.join(tempDir, 'pbvex', '_generated', 'dataModel.ts'), 'utf-8');
    expect(generatedDataModel).toContain("export type TableNames = 'messages'");
    expect(generatedDataModel).toContain('"_id": Id<"messages">');
    expect(generatedDataModel).toContain('"_creationTime": number');

    const help = execSync(`node ${cliPath} migrations --help`, { cwd: tempDir, encoding: 'utf8' });
    expect(help).toContain('create');
    expect(help).toContain('plan');
    expect(help).toContain('pocketbase');
    expect(() => execSync(`node ${cliPath} migrate create legacy`, { cwd: tempDir, stdio: 'pipe' })).toThrow();

    await writeFile(
      path.join(tempDir, 'pbvex', 'schema.ts'),
      `import { defineSchema, defineTable } from 'pbvex/server';\nimport { v } from 'pbvex/values';\nexport default defineSchema({\n  messages: defineTable({ body: v.string(), channel: v.optional(v.string()) }),\n});\n`,
      'utf-8',
    );

    const createOutput = execSync(
      `node ${cliPath} migrations create add-channel --table messages --active-artifact ${activeArtifactPath}`,
      { cwd: tempDir, encoding: 'utf8' },
    );
    expect(createOutput).toContain('Created pbvex/migrations/');
    const migrationPath = path.join(tempDir, 'pbvex', 'migrations');
    const migrationName = (await readdir(migrationPath))[0]!;
    expect(await readFile(path.join(migrationPath, migrationName), 'utf8')).toContain('defineMigration');

    execSync(`node ${cliPath} build`, { cwd: tempDir, stdio: 'pipe' });
    const plan = execSync(
      `node ${cliPath} migrations plan --active-artifact ${activeArtifactPath}`,
      { cwd: tempDir, encoding: 'utf8' },
    );
    expect(plan).toContain('Source deployment: dep_');
    expect(plan).toContain('Discovered migration matches:');
    expect(plan).toContain('_add_channel (messages, transactional)');
    expect(plan).not.toMatch(/count|estimate/i);

    await rm(tempDir, { recursive: true, force: true });
  });
});
