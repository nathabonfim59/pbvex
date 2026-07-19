import type { GenericId, ObjectValidatorFor, Validator } from '../runtime/values.js';
import { ValidationError, isValidator, v } from '../runtime/values.js';
import { isIdentifier, isPlainObject, isSafeFieldName } from '@pbvex/protocol';

export const TABLE_KIND = 'table' as const;
export const SCHEMA_KIND = 'schema' as const;

/**
 * All valid field paths for an index, including dotted paths that descend into
 * nested plain-object validators. Arrays, primitives, and branded-string ids are
 * leaves. Mirrors the runtime {@link fieldPathExists} resolution so the type
 * constraint and the validator agree on which paths are indexable.
 */
type IndexableFieldPaths<Fields extends Record<string, Validator<any>>> = {
  [K in Extract<keyof Fields, string>]: ValidatorPaths<Fields[K], K>;
}[Extract<keyof Fields, string>];

type ValidatorPaths<V, Prefix extends string> = V extends Validator<infer T>
  ? Prefix | (DocPaths<T> extends infer Sub ? Sub extends string ? `${Prefix}.${Sub}` : never : never)
  : Prefix;

// Dotted paths through a validator's inferred document type. Optional/defaulted
// wrappers are unwrapped (their inner object is indexable). Uses non-distributive
// checks so `any` and unions are leaves (the runtime cannot resolve a dotted
// path through an any or a non-deterministic union branch).
type DocPaths<T> = DocPathsOf<NonNullable<T>>;

type DocPathsOf<T> =
  [T] extends [never] ? never :
  [T] extends [string] ? never :
  [T] extends [any[]] ? never :
  [T] extends [object]
    ? {
        [K in Extract<keyof T, string>]:
          [NonNullable<T[K]>] extends [string] ? K :
          [NonNullable<T[K]>] extends [any[]] ? K :
          [NonNullable<T[K]>] extends [object] ? K | `${K}.${DocPathsOf<NonNullable<T[K]>>}` : K;
      }[Extract<keyof T, string>]
    : never;

export interface IndexDefinitionJson {
  readonly name: string;
  readonly fields: readonly string[];
}

export interface IndexDefinition extends IndexDefinitionJson {
  readonly fields: readonly string[];
  toJSON(): IndexDefinitionJson;
}

export interface TableDefinitionJson {
  tableName: string;
  fields: Record<string, unknown>;
  indexes?: IndexDefinitionJson[];
}

export interface TableDefinition<Fields extends Record<string, Validator<any>> = Record<string, Validator<any>>> {
  readonly kind: typeof TABLE_KIND;
  readonly tableName: string;
  readonly fields: Readonly<Fields>;
  readonly indexes?: readonly IndexDefinition[];
  index<KS extends readonly IndexableFieldPaths<Fields>[]>(name: string, fields: KS): TableDefinition<Fields>;
  toJSON(): TableDefinitionJson;
}

type DocumentValidatorFields<Fields extends Record<string, Validator<any>>, TableName extends string> =
  Omit<Fields, '_id' | '_creationTime'> & {
    _id: Validator<GenericId<TableName>>;
    _creationTime: Validator<number>;
  };

export type SchemaTableDefinition<
  TableName extends string,
  Fields extends Record<string, Validator<any>> = Record<string, Validator<any>>,
> = TableDefinition<Fields> & {
  readonly tableName: TableName;
  readonly documentValidator: ObjectValidatorFor<DocumentValidatorFields<Fields, TableName>>;
};

type BoundTable<Table, Name extends string> = Table extends TableDefinition<infer Fields>
  ? SchemaTableDefinition<Name, Fields>
  : never;

type BoundTables<Tables extends Record<string, TableDefinition>> = {
  [Name in keyof Tables]: Name extends string ? BoundTable<Tables[Name], Name> : never;
};

export interface SchemaDefinitionJson {
  tables: TableDefinitionJson[];
}

export interface SchemaDefinition<Tables extends Record<string, TableDefinition> = Record<string, TableDefinition>> {
  readonly kind: typeof SCHEMA_KIND;
  readonly tables: Readonly<BoundTables<Tables> & Record<string, SchemaTableDefinition<string> | undefined>>;
  readonly tableNames: readonly (keyof Tables & string)[];
  toJSON(): SchemaDefinitionJson;
  getTable<Name extends keyof Tables & string>(name: Name): BoundTable<Tables[Name], Name>;
}

function codeUnitCompare(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

function assertPlainObject(value: unknown, path: string): void {
  if (typeof value !== 'object' || value === null) {
    throw new ValidationError(`Expected an object at ${path}`);
  }
  if (!isPlainObject(value)) {
    throw new ValidationError(`Unsupported object prototype at ${path}`);
  }
}

export function index(name: string, fields: readonly string[]): IndexDefinition {
  if (!isIdentifier(name)) {
    throw new ValidationError(`Invalid index name: ${JSON.stringify(name)}`);
  }
  if (!Array.isArray(fields) || fields.length === 0) {
    throw new ValidationError(`Index fields must be a non-empty array of strings`);
  }
  const seen = new Set<string>();
  for (const field of fields) {
    if (typeof field !== 'string') {
      throw new ValidationError(`Index field must be a string`);
    }
    if (!isSafeFieldName(field)) {
      throw new ValidationError(`Invalid index field name: ${JSON.stringify(field)}`);
    }
    if (seen.has(field)) {
      throw new ValidationError(`Duplicate field ${JSON.stringify(field)} in index ${JSON.stringify(name)}`);
    }
    seen.add(field);
  }
  const fieldsCopy = Object.freeze([...fields]) as readonly string[];
  return Object.freeze({
    name,
    fields: fieldsCopy,
    toJSON(): IndexDefinitionJson {
      return { name: this.name, fields: [...this.fields] };
    },
  }) as IndexDefinition;
}

function validateIndexes<Fields extends Record<string, Validator<any>>>(
  indexes: readonly IndexDefinition[],
  fields: Readonly<Fields>,
  tableName: string,
): IndexDefinition[] {
  const names = new Set<string>();
  const fieldLists = new Set<string>();

  for (const idx of indexes) {
    if (typeof idx !== 'object' || idx === null) {
      throw new ValidationError(`Invalid index in table ${JSON.stringify(tableName)}`);
    }
    if (!isIdentifier(idx.name)) {
      throw new ValidationError(`Invalid index name ${JSON.stringify(idx.name)} in table ${JSON.stringify(tableName)}`);
    }
    if (names.has(idx.name)) {
      throw new ValidationError(`Duplicate index name ${JSON.stringify(idx.name)} in table ${JSON.stringify(tableName)}`);
    }
    names.add(idx.name);

    if (!Array.isArray(idx.fields) || idx.fields.length === 0) {
      throw new ValidationError(`Index ${JSON.stringify(idx.name)} fields must be a non-empty array of strings`);
    }

    const seenFields = new Set<string>();
    for (const field of idx.fields) {
      if (typeof field !== 'string') {
        throw new ValidationError(`Index ${JSON.stringify(idx.name)} field must be a string`);
      }
      if (!isSafeFieldName(field)) {
        throw new ValidationError(`Invalid index field name ${JSON.stringify(field)} in table ${JSON.stringify(tableName)}`);
      }
      if (seenFields.has(field)) {
        throw new ValidationError(`Duplicate field ${JSON.stringify(field)} in index ${JSON.stringify(idx.name)}`);
      }
      if (!fieldPathExists(fields, field)) {
        throw new ValidationError(`Index ${JSON.stringify(idx.name)} references absent field ${JSON.stringify(field)} in table ${JSON.stringify(tableName)}`);
      }
      seenFields.add(field);
    }

    const listKey = JSON.stringify(idx.fields);
    if (fieldLists.has(listKey)) {
      throw new ValidationError(`Duplicate index fields ${listKey} in table ${JSON.stringify(tableName)}`);
    }
    fieldLists.add(listKey);
  }

  return indexes as IndexDefinition[];
}

/**
 * Resolves a dotted field path against a table's field validators, descending
 * into nested `object` validators. Returns true when every segment resolves to
 * an existing field (top-level fields use a single segment).
 */
function fieldPathExists(fields: Readonly<Record<string, Validator<any>>>, path: string): boolean {
  const parts = path.split('.');
  if (parts.length === 0 || parts.some((p) => !isSafeFieldName(p))) return false;
  let node: unknown = fields[parts[0]!];
  for (let i = 1; i < parts.length; i++) {
    const shape = objectShapeOf(node);
    if (!shape) return false;
    node = (shape as Record<string, unknown>)[parts[i]!];
  }
  return node !== undefined;
}

// Returns the `shape`/`fields` map of an object validator, unwrapping
// optional/defaulted wrappers first (mirrors the backend ObjectFieldValidator
// so type-level and runtime path resolution agree). Accepts either a validator
// instance (with toJSON) or an already-serialized plain descriptor.
function objectShapeOf(validator: unknown): Record<string, unknown> | undefined {
  let desc: unknown = validator;
  if (
    desc !== null &&
    typeof desc === 'object' &&
    typeof (desc as { toJSON?: unknown }).toJSON === 'function'
  ) {
    desc = (desc as { toJSON: () => unknown }).toJSON();
  }
  for (let guard = 0; guard < 64; guard++) {
    if (desc === null || typeof desc !== 'object') return undefined;
    const json = desc as Record<string, unknown>;
    const type = json.type;
    if (typeof type !== 'string') return undefined;
    if (type === 'optional' || type === 'defaulted') {
      desc = json.validator;
      continue;
    }
    if (type !== 'object') return undefined;
    const shape = json.shape ?? json.fields;
    return shape && typeof shape === 'object' ? (shape as Record<string, unknown>) : undefined;
  }
  return undefined;
}

function validateFields<Fields extends Record<string, Validator<any>>>(fields: Readonly<Fields>, tableName: string): void {
  for (const key of Object.keys(fields)) {
    if (!isSafeFieldName(key)) {
      throw new ValidationError(`Invalid field name ${JSON.stringify(key)} in table ${JSON.stringify(tableName)}`);
    }
    if (!isValidator(fields[key])) {
      throw new ValidationError(`Invalid validator for field ${JSON.stringify(key)} in table ${JSON.stringify(tableName)}`);
    }
    if (key === '_id' || key === '_creationTime' || key.startsWith('_pbvex_')) {
      throw new ValidationError(`Reserved system field ${JSON.stringify(key)} in table ${JSON.stringify(tableName)}`);
    }
  }
}

function createTableDefinition<Fields extends Record<string, Validator<any>>>(
  fields: Readonly<Fields>,
  options: { indexes?: readonly IndexDefinition[] } | undefined,
  tableName: string,
): TableDefinition<Fields> {
  assertPlainObject(fields, 'fields');
  validateFields(fields, tableName);
  const fieldsCopy = Object.freeze({ ...fields }) as Readonly<Fields>;

  const indexes = options?.indexes
    ? (Object.freeze(
        validateIndexes(
          Array.isArray(options.indexes)
            ? [...options.indexes].map((idx) => index(idx.name, idx.fields))
            : (() => {
                throw new ValidationError(
                  `indexes must be an array in table ${JSON.stringify(tableName)}`,
                );
              })(),
          fieldsCopy,
          tableName,
        ),
      ) as readonly IndexDefinition[])
    : undefined;

  const table: TableDefinition<Fields> & { documentValidator?: ObjectValidatorFor<any> } = {
    kind: TABLE_KIND,
    tableName,
    fields: fieldsCopy,
    indexes,
    index<KS extends readonly IndexableFieldPaths<Fields>[]>(name: string, fieldsList: KS): TableDefinition<Fields> {
      const newIndex = index(name, fieldsList);
      return createTableDefinition(this.fields, { indexes: [...(this.indexes ?? []), newIndex] }, this.tableName);
    },
    toJSON(): TableDefinitionJson {
      const json: TableDefinitionJson = {
        tableName: this.tableName,
        fields: Object.fromEntries(
          Object.entries(this.fields)
            .map(([key, validator]) => [key, validator.toJSON()])
            .sort(([a], [b]) => codeUnitCompare(String(a), String(b))),
        ),
      };
      if (this.indexes && this.indexes.length > 0) {
        json.indexes = [...this.indexes]
          .sort((a, b) => codeUnitCompare(a.name, b.name))
          .map((idx) => (idx.toJSON ? idx.toJSON() : idx));
      }
      return json;
    },
  };

  if (tableName !== '') {
    table.documentValidator = Object.freeze(v.object({
      ...fieldsCopy,
      _id: v.id(tableName),
      _creationTime: v.number(),
    }));
  }

  return Object.freeze(table) as TableDefinition<Fields>;
}

export function defineTable<Fields extends Record<string, Validator<any>>>(
  fields: Readonly<Fields> & { readonly _id?: never; readonly _creationTime?: never },
  options?: { indexes?: readonly IndexDefinition[] },
): TableDefinition<Fields> {
  return createTableDefinition<Fields>(fields, options, '');
}

export function defineSchema<Tables extends Record<string, TableDefinition>>(
  tables: Tables,
): SchemaDefinition<Tables> {
  assertPlainObject(tables, 'tables');
  const tableNames = (Object.keys(tables) as (keyof Tables & string)[]).sort(codeUnitCompare);
  Object.freeze(tableNames);
  const schemaTables: Record<string, TableDefinition> = {};

  for (const name of tableNames) {
    if (!isIdentifier(name)) {
      throw new ValidationError(`Invalid table name: ${JSON.stringify(name)}`);
    }
    const table = tables[name];
    if (!isTableDefinition(table)) {
      throw new ValidationError(`Invalid table definition for ${JSON.stringify(name)}`);
    }
    schemaTables[name as string] = createTableDefinition(table.fields, { indexes: table.indexes }, name as string);
  }

  Object.freeze(schemaTables);

  return Object.freeze({
    kind: SCHEMA_KIND,
    tables: schemaTables,
    tableNames,
    toJSON(): SchemaDefinitionJson {
      return {
        tables: this.tableNames.map((name) => (this.tables[name] as TableDefinition).toJSON() as TableDefinitionJson),
      };
    },
    getTable<Name extends keyof Tables & string>(name: Name): BoundTable<Tables[Name], Name> {
      return this.tables[name] as BoundTable<Tables[Name], Name>;
    },
  }) as unknown as SchemaDefinition<Tables>;
}

export function isTableDefinition(value: unknown): value is TableDefinition {
  if (value === null || typeof value !== 'object') return false;
  const obj = value as Record<string, unknown>;
  return (
    obj.kind === TABLE_KIND &&
    typeof obj.fields === 'object' &&
    obj.fields !== null &&
    (typeof obj.indexes === 'undefined' || Array.isArray(obj.indexes))
  );
}

export function isSchemaDefinition(value: unknown): value is SchemaDefinition {
  if (value === null || typeof value !== 'object') return false;
  const obj = value as Record<string, unknown>;
  return obj.kind === SCHEMA_KIND && typeof obj.tables === 'object' && obj.tables !== null;
}
