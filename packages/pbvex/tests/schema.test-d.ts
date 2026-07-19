import { expectTypeOf } from 'vitest';
import { defineSchema, defineTable, type SchemaDefinition, type SchemaTableDefinition, type TableDefinition } from '../src/schema/schema.js';
import { v, type ObjectValidator, type Validator } from '../src/runtime/values.js';
import { paginationResultValidator, type Id } from '../src/runtime/server.js';

type OutputOf<T extends Validator<any, any>> = T['__type'];
type InputOf<T extends Validator<any, any>> = NonNullable<T['__inputType']>;

const table = defineTable({ name: v.string(), email: v.optional(v.string()) });
const indexed = table.index('by_email', ['email']);
expectTypeOf(indexed).toEqualTypeOf<typeof table>();

const table2 = defineTable({ name: v.string() });
// @ts-expect-error email is not a field of table2
const bad = table2.index('by_email', ['email']);

const schema = defineSchema({
  users: defineTable({ name: v.string(), count: v.number() }).index('by_name', ['name']),
  messages: defineTable({ body: v.string() }),
});

expectTypeOf(schema).toEqualTypeOf<
  SchemaDefinition<{
    users: TableDefinition<{ name: Validator<string>; count: Validator<number> }>;
    messages: TableDefinition<{ body: Validator<string> }>;
  }>
>();

expectTypeOf(schema.tableNames).toEqualTypeOf<readonly ('messages' | 'users')[]>();
expectTypeOf(schema.tables.users.fields.name).toEqualTypeOf<Validator<string>>();
expectTypeOf(schema.tables.users.fields.count).toEqualTypeOf<Validator<number>>();
expectTypeOf(schema.tables.messages.fields.body).toEqualTypeOf<Validator<string>>();
expectTypeOf(schema.getTable('users')).toEqualTypeOf<typeof schema.tables.users>();
expectTypeOf(schema.getTable('users').fields.name).toEqualTypeOf<Validator<string>>();
expectTypeOf(schema.tables['missing']).toEqualTypeOf<SchemaTableDefinition<string> | undefined>();
// @ts-expect-error missing is not a known table
schema.getTable('missing');

const photoSchema = defineSchema({
  photos: defineTable({
    url: v.string(),
    caption: v.optional(v.string()),
    width: v.defaulted(v.number(), 0),
  }),
});
const photoDocument = photoSchema.tables.photos.documentValidator;
expectTypeOf(photoDocument).toMatchTypeOf<ObjectValidator>();
expectTypeOf<OutputOf<typeof photoDocument>>().toEqualTypeOf<{
  url: string;
  caption: string | undefined;
  width: number;
  _id: Id<'photos'>;
  _creationTime: number;
}>();
type PhotoDocumentInput = {
  url: string;
  caption?: string;
  width?: number;
  _id: Id<'photos'>;
  _creationTime: number;
};
expectTypeOf<InputOf<typeof photoDocument>>().toMatchTypeOf<PhotoDocumentInput>();
expectTypeOf<PhotoDocumentInput>().toMatchTypeOf<InputOf<typeof photoDocument>>();
expectTypeOf(photoSchema.getTable('photos').documentValidator).toEqualTypeOf(photoDocument);
const photoPage = paginationResultValidator(photoDocument);
expectTypeOf<OutputOf<typeof photoPage>['page'][number]>().toEqualTypeOf<OutputOf<typeof photoDocument>>();
expectTypeOf<InputOf<typeof photoPage>['page'][number]>().toEqualTypeOf<InputOf<typeof photoDocument>>();
expectTypeOf(photoDocument.pick('_id', 'url').validate({})).toEqualTypeOf<{ _id: Id<'photos'>; url: string }>();
expectTypeOf(photoDocument.omit('_creationTime').extend({ selected: v.boolean() }).partial()).toMatchTypeOf<ObjectValidator>();
// @ts-expect-error complete document IDs retain the schema table name
const wrongPhotoId: Id<'users'> = photoDocument.validate({} as never)._id;
// @ts-expect-error system fields cannot be declared by callers
defineTable({ _id: v.string() });

// runtime immutability is reflected in the readonly API
// @ts-expect-error schema.tables is readonly
schema.tables = {} as any;
// @ts-expect-error schema.tables is readonly
schema.tables.users = {} as any;
// @ts-expect-error schema.tables.users.fields is readonly
schema.tables.users.fields.name = v.number();
// @ts-expect-error schema.tables.users.fields is readonly
schema.tables.users.fields = {} as any;
// @ts-expect-error schema.tableNames is readonly
schema.tableNames.push('x');
// @ts-expect-error schema.tables.users.indexes is readonly
schema.tables.users.indexes?.push(index('by_name', ['name']));
// @ts-expect-error index.fields is readonly
schema.tables.users.indexes![0].fields.push('x');
// @ts-expect-error getTable result is readonly
schema.getTable('users').fields.name = v.number();
