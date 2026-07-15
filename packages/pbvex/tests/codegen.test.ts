import { describe, it, expect } from 'vitest';
import { generateApiTs, generateDataModelTs, generateServerTs } from '../src/codegen/codegen.js';
import { validatorToTypeString } from '../src/codegen/types.js';
import { deriveFunctionName } from '../src/bundler/functionName.js';
import { v } from '../src/runtime/values.js';
import { defineSchema, defineTable } from '../src/schema/schema.js';

describe('codegen', () => {
  it('generates nested api and internal objects with FunctionReference metadata', () => {
    const fakeFn: any = {
      type: 'query',
      visibility: 'public',
      modulePath: 'pbvex/messages.ts',
      exportName: 'list',
      args: { toJSON: () => ({ type: 'object', shape: { channel: { type: 'string' } } }) },
      returns: { toJSON: () => ({ type: 'array', item: { type: 'string' } }) },
      handler: () => {},
    };
    const internalFn: any = {
      type: 'query',
      visibility: 'internal',
      modulePath: 'pbvex/messages.ts',
      exportName: 'internalStats',
      args: { toJSON: () => ({ type: 'object', shape: {} }) },
      returns: { toJSON: () => ({ type: 'number' }) },
      handler: () => {},
    };
    const output = generateApiTs([fakeFn, internalFn]);
    expect(output).toContain("import type { FunctionReference } from 'pbvex/server'");
    expect(output).toContain('export const api');
    expect(output).toContain('export const internal');
    expect(output).toContain('"messages": {');
    expect(output).toContain('"list": { "_path": "pbvex_messages_list_');
    expect(output).toContain('"internalStats": { "_path": "pbvex_messages_internalStats_');
    expect(output).toContain('"_visibility": "public"');
    expect(output).toContain('"_visibility": "internal"');
    expect(output).toContain('export type ApiDefinition = typeof api');
    expect(output).toContain('export type InternalApiDefinition = typeof internal');
  });

  it('generates Doc, Id, TableNames and DataModel from schema', () => {
    const schema = defineSchema({
      users: defineTable({
        name: v.string(),
        email: v.string(),
        role: v.union(v.literal('admin'), v.literal('user')),
      }),
      messages: defineTable({
        body: v.string(),
        channel: v.optional(v.string()),
      }),
    });
    const output = generateDataModelTs(schema);
    expect(output).toContain("export type TableNames = 'messages' | 'users'");
    expect(output).toContain("export type Doc<TableName extends TableNames> = DataModel[TableName]['document']");
    expect(output).toContain('export type Id<TableName extends TableNames> = GenericId<TableName>');
    expect(output).toContain('export interface users extends GenericDocument {\n  "_id": Id<"users">;\n  "_creationTime": number;\n  "email": string;\n  "name": string;\n  "role": "admin" | "user";\n}');
    expect(output).toContain('export interface messages extends GenericDocument {\n  "_id": Id<"messages">;\n  "_creationTime": number;\n  "body": string;\n  "channel"?: string | undefined;\n}');
    expect(output).toContain('export interface messagesInsert {\n  "body": string;\n  "channel"?: string | undefined;\n}');
    expect(output).toContain('export interface usersInsert {');
    expect(output).toContain('export type DataModel = {');
    expect(output).toContain('"messages": TableInfo<messages, never, messagesInsert>');
    expect(output).toContain('"users": TableInfo<users, never, usersInsert>');
  });

  it('marks defaulted fields required in documents but optional in inserts', () => {
    const schema = defineSchema({
      items: defineTable({
        name: v.string(),
        count: v.defaulted(v.number(), 0),
        tag: v.optional(v.string()),
      }),
    });
    const output = generateDataModelTs(schema);
    expect(output).toContain('export interface items extends GenericDocument {');
    expect(output).toContain('"count": number;');
    expect(output).toContain('export interface itemsInsert {');
    expect(output).toContain('"count"?: number;');
    expect(output).toContain('"tag"?: string | undefined;');
  });

  it('generates server context types', () => {
    const output = generateServerTs();
    expect(output).toContain('export type QueryCtx');
    expect(output).toContain('export type MutationCtx');
    expect(output).toContain('export type ActionCtx');
    expect(output).toContain('export type HttpActionCtx');
    expect(output).toContain('export type EmailTemplateName = never;');
    expect(output).toContain('StorageReader, StorageContext, StorageId, EmailContext, EmailSendOptions, EmailTemplateVariables, HttpContext, HttpSendOptions, HttpSendResponse');
    expect(output).toContain('export const httpAction');
    expect(output).not.toContain('internalHttpAction');
  });

  it('maps new validators and encoded bigint literals to type strings', () => {
    expect(validatorToTypeString(v.int64().toJSON())).toBe('bigint');
    expect(validatorToTypeString(v.bytes().toJSON())).toBe('ArrayBuffer');
    expect(validatorToTypeString(v.float64().toJSON())).toBe('number');
    expect(validatorToTypeString(v.number().toJSON())).toBe('number');
    expect(validatorToTypeString(v.literal(42n).toJSON())).toBe('42n');
    expect(validatorToTypeString(v.id('users').toJSON())).toBe('Id<"users">');
  });

  it('quotes object keys and derives record key types', () => {
    expect(validatorToTypeString(v.object({ 'my-key': v.string() }).toJSON())).toBe('{ "my-key": string }');
    expect(validatorToTypeString(v.record(v.literal('foo'), v.string()).toJSON())).toBe('Record<"foo", string>');
    expect(validatorToTypeString(v.record(v.string(), v.string()).toJSON())).toBe('Record<string, string>');
  });

  it('derives deterministic, hash-suffixed function names', () => {
    const once = deriveFunctionName('pbvex/messages.ts', 'list');
    const again = deriveFunctionName('pbvex/messages.ts', 'list');
    expect(once).toBe(again);
    expect(once).toMatch(/^pbvex_messages_list_[a-z0-9]+$/);
    expect(deriveFunctionName('pbvex/foo-bar.ts', 'list')).toMatch(/^pbvex_foo_bar_list_[a-z0-9]+$/);
    expect(deriveFunctionName('pbvex/foo.bar.ts', 'list')).toMatch(/^pbvex_foo_bar_list_[a-z0-9]+$/);
    expect(deriveFunctionName('pbvex/foo-bar.ts', 'list')).not.toBe(deriveFunctionName('pbvex/foo.bar.ts', 'list'));
  });

  it('throws ValidationError on API tree collisions', () => {
    const functions: any[] = [
      { type: 'query', visibility: 'public', modulePath: 'pbvex/foo.ts', exportName: 'bar', args: v.any(), returns: v.any(), handler: () => {} },
      { type: 'query', visibility: 'public', modulePath: 'pbvex/foo/bar.ts', exportName: 'baz', args: v.any(), returns: v.any(), handler: () => {} },
    ];
    expect(() => generateApiTs(functions)).toThrow();
    const duplicate: any[] = [
      { type: 'query', visibility: 'public', modulePath: 'pbvex/messages.ts', exportName: 'list', args: v.any(), returns: v.any(), handler: () => {} },
      { type: 'query', visibility: 'public', modulePath: 'pbvex/messages.ts', exportName: 'list', args: v.any(), returns: v.any(), handler: () => {} },
    ];
    expect(() => generateApiTs(duplicate)).toThrow();
  });

  it('renders distinct api namespaces for hyphen and dot module segments', () => {
    const functions: any[] = [
      {
        type: 'query',
        visibility: 'public',
        modulePath: 'pbvex/foo-bar.ts',
        exportName: 'list',
        args: v.object({ channel: v.string() }),
        returns: v.array(v.string()),
        handler: async () => [],
      },
      {
        type: 'query',
        visibility: 'public',
        modulePath: 'pbvex/foo.bar.ts',
        exportName: 'get',
        args: v.any(),
        returns: v.string(),
        handler: async () => 'x',
      },
    ];
    const output = generateApiTs(functions);
    expect(output).toContain('"foo-bar":');
    expect(output).toContain('"foo.bar":');
    expect(output).toContain('"list"');
    expect(output).toContain('"get"');
  });

  it('maps empty object validator to Record<string, never>', () => {
    expect(validatorToTypeString(v.object({}).toJSON())).toBe('Record<string, never>');
  });

  it('emits __noArgs on no-args/empty-args refs and omits it for real args', () => {
    const noArgs = {
      type: 'query', visibility: 'public', modulePath: 'pbvex/health.ts', exportName: 'ping',
      args: v.object({}), returns: v.boolean(), handler: async () => true,
    } as any;
    const withArgs = {
      type: 'query', visibility: 'public', modulePath: 'pbvex/health.ts', exportName: 'stats',
      args: v.object({ userId: v.string() }), returns: v.number(), handler: async () => 0,
    } as any;
    const output = generateApiTs([noArgs, withArgs]);
    // No-args ref carries the discriminator; real-args ref does not.
    expect(output).toContain('"ping": { "_path":');
    const pingLine = output.split('\n').find((l) => l.includes('"ping"'))!;
    expect(pingLine).toContain('"__noArgs": true');
    const statsLine = output.split('\n').find((l) => l.includes('"stats"'))!;
    expect(statsLine).not.toContain('__noArgs');
  });

  it('emits stable named recursive type aliases for recursive/ref descriptors', () => {
    let tree: any;
    tree = v.recursive('Node', () =>
      v.object({ name: v.string(), children: v.array(tree) }),
    );
    const schema = defineSchema({
      trees: defineTable({ root: tree }),
    });
    const output = generateDataModelTs(schema);
    // A stable named alias is hoisted instead of collapsing to `unknown`.
    expect(output).toContain('export type PBVexRecursive_Node = ');
    expect(output).toContain('Array<PBVexRecursive_Node>');
    // The document field references the named alias, not `unknown`.
    expect(output).toContain('"root": PBVexRecursive_Node;');
  });

  it('produces byte-identical output regardless of nested object field order in recursive types', () => {
    let treeA: any;
    treeA = v.recursive('Node', () => v.object({ a: v.string(), b: v.number(), c: v.array(treeA) }));
    let treeB: any;
    treeB = v.recursive('Node', () => v.object({ c: v.array(treeB), b: v.number(), a: v.string() }));
    const schemaA = defineSchema({ trees: defineTable({ root: treeA }) });
    const schemaB = defineSchema({ trees: defineTable({ root: treeB }) });
    expect(generateDataModelTs(schemaA)).toBe(generateDataModelTs(schemaB));
  });

  it('produces byte-identical output regardless of nested args field order', () => {
    const functionsA: any[] = [
      { type: 'query', visibility: 'public', modulePath: 'pbvex/a.ts', exportName: 'default',
        args: v.object({ z: v.string(), a: v.number() }), returns: v.null(), handler: async () => null },
    ];
    const functionsB: any[] = [
      { type: 'query', visibility: 'public', modulePath: 'pbvex/a.ts', exportName: 'default',
        args: v.object({ a: v.number(), z: v.string() }), returns: v.null(), handler: async () => null },
    ];
    expect(generateApiTs(functionsA)).toBe(generateApiTs(functionsB));
  });

  it('generates component paths under api.components', () => {
    const fakeFn: any = {
      type: 'mutation',
      visibility: 'public',
      modulePath: 'pbvex/components/counter/messages.ts',
      exportName: 'add',
      args: { toJSON: () => ({ type: 'object', shape: {} }) },
      returns: { toJSON: () => ({ type: 'string' }) },
      handler: () => {},
    };
    const output = generateApiTs([fakeFn]);
    expect(output).toContain('"components": {');
    expect(output).toContain('"counter": {');
    expect(output).toContain('"add": { "_path": "pbvex_components_counter_messages_add_');
		expect(output).toContain('"_type": "mutation", "_visibility": "public"');
  });
});
