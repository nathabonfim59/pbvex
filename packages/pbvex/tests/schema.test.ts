import { describe, it, expect } from 'vitest';
import { validateManifest } from '@pbvex/protocol';
import { formatOpaqueId } from '@pbvex/protocol';
import { v, ValidationError } from '../src/runtime/values.js';
import { defineSchema, defineTable, index, isSchemaDefinition, isTableDefinition } from '../src/schema/schema.js';

describe('schema authoring', () => {
  it('defines a schema and serializes to protocol SchemaDescriptor shape', () => {
    const schema = defineSchema({
      users: defineTable({
        name: v.string(),
        email: v.string(),
      }).index('by_email', ['email']),
      messages: defineTable({
        body: v.string(),
        author: v.id('users'),
        channel: v.optional(v.string()),
      }),
    });

    expect(schema.kind).toBe('schema');
    expect(schema.tableNames).toEqual(['messages', 'users']);
    expect(schema.getTable('users')).toBeDefined();
    expect(schema.tables['not_there']).toBeUndefined();

    const json = schema.toJSON();
    expect(json).toEqual({
      tables: [
        {
          tableName: 'messages',
          fields: {
            author: { type: 'id', tableName: 'users' },
            body: { type: 'string' },
            channel: { type: 'optional', validator: { type: 'string' } },
          },
        },
        {
          tableName: 'users',
          fields: {
            email: { type: 'string' },
            name: { type: 'string' },
          },
          indexes: [{ name: 'by_email', fields: ['email'] }],
        },
      ],
    });
  });

  it('derives complete document validators from schema-bound tables', () => {
    const sourceFields = {
      title: v.string(),
      caption: v.optional(v.string()),
      views: v.defaulted(v.number(), 0),
    };
    const schema = defineSchema({ photos: defineTable(sourceFields) });
    const validator = schema.tables.photos.documentValidator;
    const photoId = formatOpaqueId({
      version: 2,
      keyId: 1n,
      namespace: 'root',
      table: 'photos',
      raw: '123456789012345',
    }, new Uint8Array(32));

    expect(validator.kind).toBe('object');
    expect(validator.toJSON()).toEqual({
      type: 'object',
      shape: {
        title: { type: 'string' },
        caption: { type: 'optional', validator: { type: 'string' } },
        views: { type: 'defaulted', validator: { type: 'number' }, defaultValue: 0 },
        _id: { type: 'id', tableName: 'photos' },
        _creationTime: { type: 'number' },
      },
    });
    expect(validator.validate({ title: 'Sunset', _id: photoId, _creationTime: 10 })).toEqual({
      title: 'Sunset',
      caption: undefined,
      views: 0,
      _id: photoId,
      _creationTime: 10,
    });
    expect(() => validator.validate({ title: 'Sunset', _id: photoId })).toThrow(ValidationError);
    expect(() => validator.validate({ title: 'Sunset', _id: photoId, _creationTime: 10, extra: true })).toThrow(ValidationError);

    expect(validator.pick('_id', 'title').toJSON()).toEqual({
      type: 'object',
      shape: { _id: { type: 'id', tableName: 'photos' }, title: { type: 'string' } },
    });
    expect(validator.omit('_creationTime').extend({ selected: v.boolean() }).partial().kind).toBe('object');
  });

  it('keeps document validators and their schema field snapshot immutable', () => {
    const fields = { name: v.string() };
    const table = defineTable(fields);
    const schema = defineSchema({ users: table });
    const validator = schema.getTable('users').documentValidator;

    fields.name = v.number();
    expect(validator.toJSON()).toEqual({
      type: 'object',
      shape: {
        name: { type: 'string' },
        _id: { type: 'id', tableName: 'users' },
        _creationTime: { type: 'number' },
      },
    });
    expect(Object.isFrozen(validator)).toBe(true);
    expect(table).not.toHaveProperty('documentValidator');
    expect(validator.omit('_id')).not.toBe(validator);
    expect(validator.toJSON()).toHaveProperty('shape._id');
  });

  it('does not include document validators in schema serialization', () => {
    const schema = defineSchema({ photos: defineTable({ url: v.string() }) });
    expect(Object.keys(schema.toJSON().tables[0]).sort()).toEqual(['fields', 'tableName']);
    expect(JSON.parse(JSON.stringify(schema))).toEqual(schema.toJSON());
  });

  it('toJSON is JSON.stringify-safe and accepted by validateManifest', () => {
    const schema = defineSchema({
      users: defineTable({
        name: v.string(),
        count: v.int64(),
        data: v.bytes(),
        flag: v.literal(42n),
        quantity: v.number(),
        score: v.float64(),
      }).index('by_name', ['name']),
    });
    const json = schema.toJSON();
    const stringified = JSON.stringify(json);
    expect(stringified).toBeTruthy();
    const parsed = JSON.parse(stringified);
    expect(() =>
      validateManifest({
        protocolVersion: 'v1',
        deploymentId: 'test',
        functions: [],
        schema: parsed,
      }),
    ).not.toThrow();
  });

  it('rejects invalid table names', () => {
    expect(() =>
      defineSchema({
        '1invalid': defineTable({ name: v.string() }),
      }),
    ).toThrow(ValidationError);
  });

  it('rejects invalid field names', () => {
    expect(() =>
      defineSchema({
        users: defineTable({ $name: v.string() } as any),
      }),
    ).toThrow(ValidationError);
  });

  it.each(['_id', '_creationTime', '_pbvex_internal'])('rejects reserved top-level system field %s', (field) => {
    expect(() => defineTable({ [field]: v.string() })).toThrow(ValidationError);
  });

  it('rejects invalid index names', () => {
    expect(() =>
      defineSchema({
        users: defineTable({ name: v.string() }, { indexes: [index('1bad', ['name'])] }),
      }),
    ).toThrow(ValidationError);
  });

  it('rejects duplicate index names', () => {
    expect(() =>
      defineSchema({
        users: defineTable(
          { name: v.string(), email: v.string() },
          {
            indexes: [index('by_email', ['email']), index('by_email', ['name'])],
          },
        ),
      }),
    ).toThrow(ValidationError);
  });

  it('rejects duplicate index fields', () => {
    expect(() =>
      defineSchema({
        users: defineTable(
          { name: v.string(), email: v.string() },
          {
            indexes: [index('by_email', ['email']), index('also_by_email', ['email'])],
          },
        ),
      }),
    ).toThrow(ValidationError);
  });

  it('rejects duplicate fields within an index', () => {
    expect(() =>
      defineSchema({
        users: defineTable({ name: v.string(), email: v.string() }, { indexes: [index('by_name_email', ['name', 'name'])] }),
      }),
    ).toThrow(ValidationError);
  });

  it('rejects indexes referencing absent fields', () => {
    expect(() =>
      defineSchema({
        users: defineTable({ name: v.string() }, { indexes: [index('by_email', ['email'])] }),
      }),
    ).toThrow(ValidationError);
  });

  it('accepts nested dotted index fields that resolve into object validators', () => {
    expect(() =>
      defineSchema({
        users: defineTable(
          { profile: v.object({ name: v.string(), age: v.number() }) },
          { indexes: [index('by_profile_name', ['profile.name']), index('by_profile_age', ['profile.age'])] },
        ),
      }),
    ).not.toThrow();
  });

  it('resolves nested index paths through optional/defaulted wrappers', () => {
    expect(() =>
      defineSchema({
        users: defineTable(
          {
            profile: v.optional(v.object({ name: v.string() })),
            settings: v.defaulted(v.object({ plan: v.string() }), { plan: 'free' }),
          },
          { indexes: [index('by_profile_name', ['profile.name']), index('by_plan', ['settings.plan'])] },
        ),
      }),
    ).not.toThrow();
    // A dotted path that does not resolve through the wrapper is rejected.
    expect(() =>
      defineSchema({
        users: defineTable(
          { profile: v.optional(v.object({ name: v.string() })) },
          { indexes: [index('bad', ['profile.missing'])] },
        ),
      }),
    ).toThrow(ValidationError);
  });

  it('rejects nested index fields that do not resolve', () => {
    expect(() =>
      defineSchema({
        users: defineTable(
          { profile: v.object({ name: v.string() }) },
          { indexes: [index('bad', ['profile.missing'])] },
        ),
      }),
    ).toThrow(ValidationError);
    expect(() =>
      defineSchema({
        users: defineTable({ name: v.string() }, { indexes: [index('bad', ['name.child'])] }),
      }),
    ).toThrow(ValidationError);
  });

  it('index method is immutable and accepts readonly tuples', () => {
    const base = defineTable({ name: v.string(), email: v.string() });
    const withIndex = base.index('by_email', ['email'] as const);

    expect(base.indexes).toBeUndefined();
    expect(withIndex.indexes).toHaveLength(1);
    expect(withIndex.indexes![0].name).toBe('by_email');

    const withTwo = withIndex.index('by_name', ['name'] as const);
    expect(withIndex.indexes).toHaveLength(1);
    expect(withTwo.indexes).toHaveLength(2);
  });

  it('defineSchema is deterministic and sorted', () => {
    const schema = defineSchema({
      zebra: defineTable({ name: v.string() }),
      apple: defineTable({ name: v.string() }),
    });
    const json = schema.toJSON();
    expect(json.tables.map((t) => t.tableName)).toEqual(['apple', 'zebra']);
  });

  it('defineSchema output matches protocol manifest validator', () => {
    const schema = defineSchema({
      users: defineTable({ name: v.string() }).index('by_name', ['name']),
    });
    const json = schema.toJSON();
    expect(json.tables[0].tableName).toBe('users');
    expect(json.tables[0].fields).toEqual({ name: { type: 'string' } });
    expect(json.tables[0].indexes).toEqual([{ name: 'by_name', fields: ['name'] }]);
  });

  it('defineSchema does not corrupt reused tables and leaves originals untouched', () => {
    const table = defineTable({ name: v.string() });
    const schema = defineSchema({ users: table, people: table });
    expect(schema.tables.users.tableName).toBe('users');
    expect(schema.tables.people.tableName).toBe('people');
    expect(table.tableName).toBe('');
    expect(schema.tables.users).not.toBe(schema.tables.people);
  });

  it('schema, tables, fields and indexes are deeply frozen', () => {
    const table = defineTable({ name: v.string(), email: v.string() }).index('by_email', ['email']);
    const schema = defineSchema({ users: table });
    expect(Object.isFrozen(schema)).toBe(true);
    expect(Object.isFrozen(schema.tables)).toBe(true);
    expect(Object.isFrozen(schema.tables.users)).toBe(true);
    expect(Object.isFrozen(schema.tables.users.fields)).toBe(true);
    expect(Object.isFrozen(schema.tables.users.indexes)).toBe(true);
    expect(Object.isFrozen(schema.tables.users.indexes![0])).toBe(true);
    expect(Object.isFrozen(schema.tables.users.indexes![0].fields)).toBe(true);
    expect(() => {
      (schema.tables as any).users = {};
    }).toThrow();
    expect(() => {
      (schema.tables.users.fields as any).name = v.number();
    }).toThrow();
    expect(() => {
      (schema.tables.users.indexes![0].fields as any).push('x');
    }).toThrow();
  });

  it('toJSON snapshots indexes rather than exposing retained arrays', () => {
    const schema = defineSchema({
      users: defineTable({ name: v.string(), email: v.string() }).index('by_email', ['email']),
    });
    const json = schema.toJSON();
    expect(json.tables[0].indexes).not.toBe(schema.tables.users.indexes);
    expect(json.tables[0].indexes![0].fields).not.toBe(schema.tables.users.indexes![0].fields);
  });

  it('rebuilds supplied option indexes so caller objects are not retained', () => {
    const supplied = index('by_name', ['name']);
    const table = defineTable({ name: v.string() }, { indexes: [supplied] });
    expect(table.indexes![0]).not.toBe(supplied);
    expect(table.indexes![0].fields).not.toBe(supplied.fields);
  });

  it('rejects custom-prototype maps but accepts null-prototype maps', () => {
    expect(() => defineTable(Object.create({ name: v.string() }) as any)).toThrow(ValidationError);
    expect(() => defineSchema(Object.create({ users: defineTable({ name: v.string() }) }) as any)).toThrow(ValidationError);

    const nullProtoFields = Object.create(null, { name: { value: v.string(), enumerable: true } });
    const nullTable = defineTable(nullProtoFields as any);
    expect(nullTable.fields.name).toBeDefined();

    const nullProtoTables = Object.create(null, { users: { value: defineTable({ name: v.string() }), enumerable: true } });
    const nullSchema = defineSchema(nullProtoTables as any);
    expect(nullSchema.tables.users.fields.name).toBeDefined();
  });

  it('tableNames and index fields are immutable', () => {
    const schema = defineSchema({
      users: defineTable({ name: v.string() }).index('by_name', ['name']),
    });
    expect(() => (schema.tableNames as any).push('x')).toThrow();
    expect(() => (schema.tables.users.indexes![0].fields as any).push('x')).toThrow();
  });

  it('guards table and schema definitions', () => {
    const table = defineTable({ name: v.string() });
    expect(isTableDefinition(table)).toBe(true);
    expect(isTableDefinition({})).toBe(false);
    const schema = defineSchema({ users: table });
    expect(isSchemaDefinition(schema)).toBe(true);
    expect(isSchemaDefinition({})).toBe(false);
  });

  it('rejects malformed table and schema construction', () => {
    expect(() => defineTable(Object.create({ custom: true }) as any)).toThrow(ValidationError);
    expect(() => defineSchema(Object.create({ custom: true }) as any)).toThrow(ValidationError);
    expect(() => defineSchema({ users: { fields: { name: v.string() } } } as any)).toThrow(ValidationError);
  });

  it('index() typing accepts nested dotted paths and rejects invalid ones (tsc)', async () => {
    const { mkdtemp, rm, writeFile } = await import('node:fs/promises');
    const { tmpdir } = await import('node:os');
    const path = await import('node:path');
    const { execSync } = await import('node:child_process');
    const { fileURLToPath } = await import('node:url');
    const REPO_ROOT = path.resolve(fileURLToPath(new URL('../../../', import.meta.url)));
    const TSC = path.resolve(REPO_ROOT, 'packages/pbvex/node_modules/typescript/bin/tsc');

    const src = `import { defineTable, index } from 'pbvex/server';
import { v } from 'pbvex/values';

const users = defineTable(
  {
    name: v.string(),
    profile: v.object({ label: v.string(), age: v.number() }),
    optProfile: v.optional(v.object({ label: v.string() })),
    defProfile: v.defaulted(v.object({ plan: v.string() }), { plan: 'free' }),
    tags: v.array(v.string()),
    anything: v.any(),
    mixed: v.union(v.string(), v.object({ label: v.string() })),
  },
  { indexes: [index('by_profile_label', ['profile.label']), index('by_profile_age', ['profile.age'])] },
);

// Valid: top-level and nested dotted paths, including through optional/defaulted.
users
  .index('by_name', ['name'])
  .index('by_label', ['profile.label'])
  .index('by_label_age', ['profile.label', 'profile.age'])
  .index('by_opt_label', ['optProfile.label'])
  .index('by_plan', ['defProfile.plan']);

// @ts-expect-error invalid nested path (field does not resolve)
users.index('bad', ['profile.missing']);
// @ts-expect-error cannot descend into a non-object leaf
users.index('bad_leaf', ['name.child']);
// @ts-expect-error cannot descend into an array element
users.index('bad_array', ['tags.0']);
// @ts-expect-error top-level field must exist
users.index('bad_top', ['nonexistent']);
// @ts-expect-error any fields do not expose dotted paths (runtime cannot resolve)
users.index('bad_any', ['anything.child']);
// @ts-expect-error union fields do not expose dotted paths (non-deterministic)
users.index('bad_union', ['mixed.label']);
`;

    const tempDir = await mkdtemp(path.join(tmpdir(), 'pbvex-index-typing-'));
    await writeFile(path.join(tempDir, 'schema.ts'), src, 'utf-8');
    await writeFile(
      path.join(tempDir, 'tsconfig.json'),
      JSON.stringify({
        compilerOptions: {
          target: 'ES2022', module: 'ESNext', moduleResolution: 'bundler',
          lib: ['ES2022', 'DOM'], strict: true, esModuleInterop: true, skipLibCheck: true,
          forceConsistentCasingInFileNames: true, resolveJsonModule: true,
          allowSyntheticDefaultImports: true, noEmit: true, baseUrl: REPO_ROOT,
          paths: {
            pbvex: ['packages/pbvex/src/runtime/index.ts'],
            'pbvex/server': ['packages/pbvex/src/runtime/server.ts'],
            'pbvex/values': ['packages/pbvex/src/runtime/values.ts'],
          },
        },
        include: ['schema.ts'],
      }, null, 2),
      'utf-8',
    );
    expect(() => execSync(`node ${TSC} --noEmit -p ${path.join(tempDir, 'tsconfig.json')}`, { stdio: 'pipe' })).not.toThrow();
    await rm(tempDir, { recursive: true, force: true });
  });
});
