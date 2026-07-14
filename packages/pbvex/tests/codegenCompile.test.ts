import { describe, it, expect } from 'vitest';
import { mkdtemp, rm, writeFile, mkdir, copyFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { execSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { bundle } from '../src/bundler/bundler.js';
import { generateApiTs, generateDataModelTs, generateServerTs } from '../src/codegen/codegen.js';
import { generateCodegenFiles } from '../src/codegen/codegen.js';
import { v } from '../src/runtime/values.js';
import { defineSchema, defineTable } from '../src/schema/schema.js';

const FIXTURE = new URL('../fixtures/example', import.meta.url).pathname;
const USAGE = new URL('./fixtures/usage.ts', import.meta.url).pathname;
const REPO_ROOT = path.resolve(fileURLToPath(new URL('../../../', import.meta.url)));
const TSC = path.resolve(REPO_ROOT, 'packages/pbvex/node_modules/typescript/bin/tsc');

async function writeTsconfig(tempDir: string) {
  const tsconfigPath = path.join(tempDir, 'tsconfig.json');
  await writeFile(
    tsconfigPath,
    JSON.stringify(
      {
        compilerOptions: {
          target: 'ES2022',
          module: 'ESNext',
          moduleResolution: 'bundler',
          lib: ['ES2022', 'DOM'],
          strict: true,
          esModuleInterop: true,
          skipLibCheck: true,
          forceConsistentCasingInFileNames: true,
          resolveJsonModule: true,
          allowSyntheticDefaultImports: true,
          noEmit: true,
          baseUrl: REPO_ROOT,
          paths: {
            pbvex: ['packages/pbvex/src/runtime/index.ts'],
            'pbvex/server': ['packages/pbvex/src/runtime/server.ts'],
            'pbvex/values': ['packages/pbvex/src/runtime/values.ts'],
          },
        },
        include: ['pbvex/**/*.ts'],
      },
      null,
      2,
    ),
    'utf-8',
  );
  return tsconfigPath;
}

describe('codegen compile', () => {
  it('generates code that compiles with tsc', async () => {
    const result = await bundle({ rootDir: FIXTURE, project: 'compile-test', target: 'local' });
    expect(result.diagnostics).toEqual([]);

    const tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-compile-'));
    const pbvexDir = path.join(tempDir, 'pbvex');
    await mkdir(pbvexDir, { recursive: true });

    // Copy fixture source files
    for (const file of ['helpers.ts', 'messages.ts', 'schema.ts', 'users.ts'] as const) {
      const src = path.join(FIXTURE, 'pbvex', file);
      const dest = path.join(pbvexDir, file);
      await copyFile(src, dest);
    }

    await generateCodegenFiles({ rootDir: tempDir, functions: result.functions, project: 'compile-test' }, result.schema);

    await copyFile(USAGE, path.join(pbvexDir, 'usage.ts'));

    const tsconfigPath = await writeTsconfig(tempDir);

    expect(() => execSync(`node ${TSC} --noEmit -p ${tsconfigPath}`, { cwd: tempDir, stdio: 'pipe' })).not.toThrow();

    await rm(tempDir, { recursive: true, force: true });
  });

  it('compiles generated api with hyphen and dot module segments', async () => {
    const tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-compile-'));
    const generatedDir = path.join(tempDir, 'pbvex', '_generated');
    await mkdir(generatedDir, { recursive: true });

    const functions = [
      {
        type: 'query' as const,
        visibility: 'public' as const,
        modulePath: 'pbvex/foo-bar.ts',
        exportName: 'list',
        args: v.object({ channel: v.string() }),
        returns: v.array(v.string()),
        handler: async () => [],
      },
      {
        type: 'query' as const,
        visibility: 'public' as const,
        modulePath: 'pbvex/foo.bar.ts',
        exportName: 'get',
        args: v.any(),
        returns: v.string(),
        handler: async () => 'x',
      },
    ];

    await writeFile(path.join(generatedDir, 'api.ts'), generateApiTs(functions as any), 'utf-8');
    await writeFile(path.join(generatedDir, 'dataModel.ts'), generateDataModelTs(undefined), 'utf-8');
    await writeFile(path.join(generatedDir, 'server.ts'), generateServerTs(), 'utf-8');

    const tsconfigPath = await writeTsconfig(tempDir);

    expect(() => execSync(`node ${TSC} --noEmit -p ${tsconfigPath}`, { cwd: tempDir, stdio: 'pipe' })).not.toThrow();

    await rm(tempDir, { recursive: true, force: true });
  });

  it('generates usable recursive document types that compile with tsc', async () => {
    const tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-recursive-'));
    const generatedDir = path.join(tempDir, 'pbvex', '_generated');
    await mkdir(generatedDir, { recursive: true });

    // A self-referential Tree validator.
    let tree: any;
    tree = v.recursive('Node', () => v.object({ name: v.string(), children: v.array(tree) }));
    const schema = defineSchema({ trees: defineTable({ root: tree }) });
    await writeFile(path.join(generatedDir, 'dataModel.ts'), generateDataModelTs(schema), 'utf-8');
    await writeFile(path.join(generatedDir, 'server.ts'), generateServerTs(), 'utf-8');

    // Usage that relies on the generated recursive shape (not `unknown`):
    // descending into nested children must typecheck.
    const usage = `import type { Doc } from './dataModel.js';
declare const doc: Doc<'trees'>;
const first: string = doc.root.name;
const grandchild: string | undefined = doc.root.children[0]?.children[0]?.name;
// @ts-expect-error a non-existent nested field is a type error
const bad: number = doc.root.children[0]?.missing;
export { first, grandchild, bad };
`;
    await writeFile(path.join(tempDir, 'usage.ts'), usage, 'utf-8');

    const tsconfigPath = await writeTsconfig(tempDir);
    expect(() => execSync(`node ${TSC} --noEmit -p ${tsconfigPath}`, { cwd: tempDir, stdio: 'pipe' })).not.toThrow();
    await rm(tempDir, { recursive: true, force: true });
  });

  it('scopes same recursive name with different shapes across tables', async () => {
    const tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-scope-'));
    const generatedDir = path.join(tempDir, 'pbvex', '_generated');
    await mkdir(generatedDir, { recursive: true });

    // Two tables each declare recursive name "Node" with DIFFERENT shapes.
    let treeA: any;
    treeA = v.recursive('Node', () => v.object({ label: v.string(), children: v.array(treeA) }));
    let treeB: any;
    treeB = v.recursive('Node', () => v.object({ title: v.number(), descendants: v.array(treeB) }));
    const schema = defineSchema({
      tableA: defineTable({ root: treeA }),
      tableB: defineTable({ root: treeB }),
    });
    const dataModel = generateDataModelTs(schema);
    await writeFile(path.join(generatedDir, 'dataModel.ts'), dataModel, 'utf-8');
    await writeFile(path.join(generatedDir, 'server.ts'), generateServerTs(), 'utf-8');

    // Both aliases must be emitted (Node and Node_2) and produce distinct types.
    expect(dataModel).toContain('export type PBVexRecursive_Node = ');
    expect(dataModel).toContain('export type PBVexRecursive_Node_2 = ');
    expect(dataModel).toContain('"root": PBVexRecursive_Node;');
    expect(dataModel).toContain('"root": PBVexRecursive_Node_2;');

    // Usage that relies on each alias having its own shape.
    const usage = `import type { Doc } from './dataModel.js';
declare const a: Doc<'tableA'>;
declare const b: Doc<'tableB'>;
const label: string = a.root.label;
const title: number = b.root.title;
// @ts-expect-error tableA's Node has label, not title
const bad: number = a.root.title;
export { label, title, bad };
`;
    await writeFile(path.join(tempDir, 'usage.ts'), usage, 'utf-8');

    const tsconfigPath = await writeTsconfig(tempDir);
    expect(() => execSync(`node ${TSC} --noEmit -p ${tsconfigPath}`, { cwd: tempDir, stdio: 'pipe' })).not.toThrow();
    await rm(tempDir, { recursive: true, force: true });
  });

  it('scopes same recursive name with different shapes across functions', async () => {
    const tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-fnscope-'));
    const generatedDir = path.join(tempDir, 'pbvex', '_generated');
    await mkdir(generatedDir, { recursive: true });

    let treeA: any;
    treeA = v.recursive('Node', () => v.object({ label: v.string(), children: v.array(treeA) }));
    let treeB: any;
    treeB = v.recursive('Node', () => v.object({ title: v.number(), descendants: v.array(treeB) }));

    const functions = [
      { type: 'query' as const, visibility: 'public' as const, modulePath: 'pbvex/a.ts', exportName: 'default',
        args: v.object({ tree: treeA }), returns: v.null(), handler: async () => null },
      { type: 'query' as const, visibility: 'public' as const, modulePath: 'pbvex/b.ts', exportName: 'default',
        args: v.object({ tree: treeB }), returns: v.null(), handler: async () => null },
    ];
    const api = generateApiTs(functions as any);
    await writeFile(path.join(generatedDir, 'api.ts'), api, 'utf-8');
    await writeFile(path.join(generatedDir, 'dataModel.ts'), generateDataModelTs(undefined), 'utf-8');
    await writeFile(path.join(generatedDir, 'server.ts'), generateServerTs(), 'utf-8');

    expect(api).toContain('export type PBVexRecursive_Node = ');
    expect(api).toContain('export type PBVexRecursive_Node_2 = ');

    const tsconfigPath = await writeTsconfig(tempDir);
    expect(() => execSync(`node ${TSC} --noEmit -p ${tsconfigPath}`, { cwd: tempDir, stdio: 'pipe' })).not.toThrow();
    await rm(tempDir, { recursive: true, force: true });
  });

  it('collects nested recursive declarations before emission', async () => {
    const tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-nested-'));
    const generatedDir = path.join(tempDir, 'pbvex', '_generated');
    await mkdir(generatedDir, { recursive: true });

    // Outer recursive contains a nested recursive with a different name.
    let outer: any;
    let inner: any;
    inner = v.recursive('Child', () => v.object({ value: v.number(), next: v.optional(inner) }));
    outer = v.recursive('Outer', () => v.object({
      label: v.string(),
      child: inner,
      siblings: v.array(outer),
    }));
    const schema = defineSchema({ graph: defineTable({ data: outer }) });
    const dataModel = generateDataModelTs(schema);
    await writeFile(path.join(generatedDir, 'dataModel.ts'), dataModel, 'utf-8');
    await writeFile(path.join(generatedDir, 'server.ts'), generateServerTs(), 'utf-8');

    // Both aliases must be emitted — the nested Child is discovered during
    // the collection pre-pass, not lost by snapshotting before body rendering.
    expect(dataModel).toContain('export type PBVexRecursive_Outer = ');
    expect(dataModel).toContain('export type PBVexRecursive_Child = ');

    const usage = `import type { Doc } from './dataModel.js';
declare const doc: Doc<'graph'>;
const label: string = doc.data.label;
const childVal: number = doc.data.child.value;
const siblingLabel: string = doc.data.siblings[0]?.label ?? '';
export { label, childVal, siblingLabel };
`;
    await writeFile(path.join(tempDir, 'usage.ts'), usage, 'utf-8');

    const tsconfigPath = await writeTsconfig(tempDir);
    expect(() => execSync(`node ${TSC} --noEmit -p ${tsconfigPath}`, { cwd: tempDir, stdio: 'pipe' })).not.toThrow();
    await rm(tempDir, { recursive: true, force: true });
  });

  it('sanitizes reserved recursive names and collisions with generated symbols', async () => {
    const tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-sanitize-'));
    const generatedDir = path.join(tempDir, 'pbvex', '_generated');
    await mkdir(generatedDir, { recursive: true });

    // "class" is a TS reserved word; "Id" collides with the generated Id type.
    let reserved: any;
    reserved = v.recursive('class', () => v.object({ name: v.string(), next: v.array(reserved) }));
    let collides: any;
    collides = v.recursive('Id', () => v.object({ value: v.number(), next: v.array(collides) }));
    const schema = defineSchema({
      items: defineTable({ cls: reserved, id: collides }),
    });
    const dataModel = generateDataModelTs(schema);
    await writeFile(path.join(generatedDir, 'dataModel.ts'), dataModel, 'utf-8');
    await writeFile(path.join(generatedDir, 'server.ts'), generateServerTs(), 'utf-8');

    // "class" is a TS reserved word; "Id" would collide with the generated Id type
    // without the PBVexRecursive_ prefix. With the prefix, both are safe.
    expect(dataModel).toContain('export type PBVexRecursive_class = ');
    expect(dataModel).toContain('export type PBVexRecursive_Id = ');
    expect(dataModel).toContain('"cls": PBVexRecursive_class;');
    expect(dataModel).toContain('"id": PBVexRecursive_Id;');

    const usage = `import type { Doc } from './dataModel.js';
declare const doc: Doc<'items'>;
const name: string = doc.cls.name;
const value: number = doc.id.value;
export { name, value };
`;
    await writeFile(path.join(tempDir, 'usage.ts'), usage, 'utf-8');

    const tsconfigPath = await writeTsconfig(tempDir);
    expect(() => execSync(`node ${TSC} --noEmit -p ${tsconfigPath}`, { cwd: tempDir, stdio: 'pipe' })).not.toThrow();
    await rm(tempDir, { recursive: true, force: true });
  });

  it('does not shadow global utility types (Array, Record, ArrayBuffer)', async () => {
    const tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-globals-'));
    const generatedDir = path.join(tempDir, 'pbvex', '_generated');
    await mkdir(generatedDir, { recursive: true });

    // Recursive names that would shadow global types without the prefix.
    let arr: any;
    arr = v.recursive('Array', () => v.object({ items: v.array(arr) }));
    let rec: any;
    rec = v.recursive('Record', () => v.object({ entries: v.array(rec) }));
    let buf: any;
    buf = v.recursive('ArrayBuffer', () => v.object({ data: v.bytes(), next: v.optional(buf) }));
    const schema = defineSchema({
      globals: defineTable({ arr, rec, buf }),
    });
    const dataModel = generateDataModelTs(schema);
    await writeFile(path.join(generatedDir, 'dataModel.ts'), dataModel, 'utf-8');
    await writeFile(path.join(generatedDir, 'server.ts'), generateServerTs(), 'utf-8');

    // Prefixed aliases must be used — no bare `export type Array = ...`.
    expect(dataModel).toContain('export type PBVexRecursive_Array = ');
    expect(dataModel).toContain('export type PBVexRecursive_Record = ');
    expect(dataModel).toContain('export type PBVexRecursive_ArrayBuffer = ');
    expect(dataModel).not.toMatch(/^export type Array = /m);
    expect(dataModel).not.toMatch(/^export type Record = /m);

    // Usage: the global Array/Record/ArrayBuffer types still work alongside
    // the recursive aliases.
    const usage = `import type { Doc } from './dataModel.js';
declare const doc: Doc<'globals'>;
const items: number = doc.arr.items.length;
const entries: number = doc.rec.entries.length;
const data: ArrayBuffer = doc.buf.data;
export { items, entries, data };
`;
    await writeFile(path.join(tempDir, 'usage.ts'), usage, 'utf-8');

    const tsconfigPath = await writeTsconfig(tempDir);
    expect(() => execSync(`node ${TSC} --noEmit -p ${tsconfigPath}`, { cwd: tempDir, stdio: 'pipe' })).not.toThrow();
    await rm(tempDir, { recursive: true, force: true });
  });

  it('produces byte-identical output regardless of field registration order', () => {
    let tree: any;
    tree = v.recursive('Node', () => v.object({ name: v.string(), children: v.array(tree) }));

    const schemaA = defineSchema({
      trees: defineTable({ root: tree, label: v.string() }),
      nodes: defineTable({ data: tree }),
    });
    const schemaB = defineSchema({
      nodes: defineTable({ data: tree }),
      trees: defineTable({ label: v.string(), root: tree }),
    });
    const outputA = generateDataModelTs(schemaA);
    const outputB = generateDataModelTs(schemaB);
    expect(outputA).toBe(outputB);
  });

  it('produces byte-identical output regardless of function registration order', () => {
    let treeA: any;
    treeA = v.recursive('Node', () => v.object({ label: v.string(), children: v.array(treeA) }));
    let treeB: any;
    treeB = v.recursive('Node', () => v.object({ title: v.number(), descendants: v.array(treeB) }));

    const functionsA: any[] = [
      { type: 'query', visibility: 'public', modulePath: 'pbvex/a.ts', exportName: 'default',
        args: v.object({ tree: treeA }), returns: v.null(), handler: async () => null },
      { type: 'query', visibility: 'public', modulePath: 'pbvex/b.ts', exportName: 'default',
        args: v.object({ tree: treeB }), returns: v.null(), handler: async () => null },
    ];
    const functionsB = [functionsA[1], functionsA[0]];
    const outputA = generateApiTs(functionsA);
    const outputB = generateApiTs(functionsB);
    expect(outputA).toBe(outputB);
  });
});
