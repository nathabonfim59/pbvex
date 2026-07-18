import { encodeValue, isCronExpression, isIdentifier } from '@pbvex/protocol';
import type { JSONValue, PbvexValue, StorageFileMetadata, StorageId, StorageImageMetadata } from '@pbvex/protocol';
import type { ObjectValidator, Validator } from './values.js';
import { v, isValidator } from './values.js';

export type { StorageFileMetadata, StorageId, StorageImageMetadata, StorageUploadResponse } from '@pbvex/protocol';

export type { TableDefinition, SchemaDefinition, IndexDefinition } from '../schema/schema.js';
export { defineSchema, defineTable, index, isSchemaDefinition, isTableDefinition } from '../schema/schema.js';

import type {
  FunctionReference,
  OptionalRestArgs,
  FunctionReturnType,
  FunctionType,
  FunctionVisibility,
  FunctionRoute,
} from '@pbvex/protocol';

export type {
  FunctionReference,
  FunctionArgs,
  OptionalRestArgs,
  FunctionReturnType,
  FunctionType,
  FunctionVisibility,
  FunctionRoute,
} from '@pbvex/protocol';

import type { GenericId } from './values.js';
import type { ResolveEnv, TypedComponent } from './component.js';
export type { GenericId };
export { defineApp, defineComponent, mount } from './component.js';

export type Visibility = 'public' | 'internal';

export type Id<TableName extends string = string> = GenericId<TableName>;

export type Value = any;
export type NumericValue = number | bigint;

type ValidatorOutput<T extends Validator<any, any>> = T extends Validator<infer Output, any> ? Output : never;
type ValidatorInput<T extends Validator<any, any>> = T extends Validator<any, infer Input> ? Input : never;
type MigrationOutput<T> = T & { readonly _id?: never; readonly _creationTime?: never };
type MigrationDocument<T, Table extends string> = T & Readonly<{
  _id: GenericId<Table>;
  _creationTime: number;
}>;

export interface MigrationContext {
  readonly migrationId: string;
  readonly activationTime: number;
  fail(message: string): never;
}

export interface MigrationDefinition<
  From extends ObjectValidator<any, any> = ObjectValidator<any, any>,
  To extends ObjectValidator<any, any> = ObjectValidator<any, any>,
  Table extends string = string,
> {
  readonly kind: 'pbvex.migration';
  readonly id: string;
  readonly table: Table;
  readonly mode: 'transactional';
  readonly from: From;
  readonly to: To;
  readonly up: (oldDoc: MigrationDocument<ValidatorOutput<From>, Table>, ctx: MigrationContext) => MigrationOutput<ValidatorInput<To>>;
  readonly down: (newDoc: MigrationDocument<ValidatorOutput<To>, Table>, ctx: MigrationContext) => MigrationOutput<ValidatorInput<From>>;
  /** @internal Populated by artifact discovery. */
  modulePath?: string;
  /** @internal Populated by artifact discovery. */
  exportName?: string;
}

export interface MigrationOptions<
  From extends ObjectValidator<any, any>,
  To extends ObjectValidator<any, any>,
  Table extends string,
> {
  id: string;
  table: Table;
  from: From;
  to: To;
  mode: 'transactional';
  up: (oldDoc: MigrationDocument<ValidatorOutput<From>, Table>, ctx: MigrationContext) => MigrationOutput<ValidatorInput<To>>;
  down: (newDoc: MigrationDocument<ValidatorOutput<To>, Table>, ctx: MigrationContext) => MigrationOutput<ValidatorInput<From>>;
}

/** Defines a pure synchronous, reversible transactional document migration. */
export function defineMigration<From extends ObjectValidator<any, any>, To extends ObjectValidator<any, any>, Table extends string>(
  options: MigrationOptions<From, To, Table>,
): MigrationDefinition<From, To, Table> {
  if (typeof options.id !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(options.id)) {
    throw new Error('Migration id must match [A-Za-z0-9][A-Za-z0-9._-]{0,127}');
  }
  if (!isIdentifier(options.table) || options.table.toLowerCase().startsWith('pbvex_cmp_')) {
    throw new Error('Migration table must be a valid user table identifier');
  }
  if (options.mode !== 'transactional') throw new Error('Migration mode must be transactional');
  if (!isValidator(options.from) || options.from.kind !== 'object' || !isValidator(options.to) || options.to.kind !== 'object') {
    throw new Error('Migration from and to must be object validators');
  }
  if (typeof options.up !== 'function' || typeof options.down !== 'function') throw new Error('Migration up and down must be functions');
  return Object.freeze({ kind: 'pbvex.migration', ...options });
}

export function isMigrationDefinition(value: unknown): value is MigrationDefinition {
  if (!value || typeof value !== 'object') return false;
  const migration = value as Partial<MigrationDefinition>;
  return migration.kind === 'pbvex.migration' && migration.mode === 'transactional' &&
    isValidator(migration.from) && migration.from.kind === 'object' &&
    isValidator(migration.to) && migration.to.kind === 'object' &&
    typeof migration.up === 'function' && typeof migration.down === 'function';
}

/** One second expressed in milliseconds. */
export const SECOND_MS = 1_000;

/** One minute expressed in milliseconds. */
export const MINUTE_MS = 60 * SECOND_MS;

/** One hour expressed in milliseconds. */
export const HOUR_MS = 60 * MINUTE_MS;

/** One 24-hour day expressed in milliseconds. */
export const DAY_MS = 24 * HOUR_MS;

/** One seven-day week expressed in milliseconds. */
export const WEEK_MS = 7 * DAY_MS;

export interface GenericDocument {
  _id: GenericId<string>;
  _creationTime: number;
}

export interface IndexInfo {
  fields: readonly string[];
}

export interface TableInfo<
  Document extends GenericDocument = GenericDocument,
  Indexes extends Record<string, IndexInfo> | never = Record<string, IndexInfo>,
  Insert extends Record<string, any> = WithoutSystemFields<Document>,
> {
  document: Document;
  insert: Insert;
  indexes?: Indexes;
}

export type GenericDataModel = Record<string, TableInfo>;

export type TableNamesInDataModel<DataModel extends GenericDataModel> = keyof DataModel & string;

export type NamedTableInfo<
  DataModel extends GenericDataModel,
  TableName extends TableNamesInDataModel<DataModel>,
> = DataModel[TableName];

export type DocumentByName<
  DataModel extends GenericDataModel,
  TableName extends TableNamesInDataModel<DataModel>,
> = DataModel[TableName]['document'];

export type InsertByName<
  DataModel extends GenericDataModel,
  TableName extends TableNamesInDataModel<DataModel>,
> = DataModel[TableName]['insert'];

export type WithoutSystemFields<Document extends GenericDocument> = Omit<Document, '_id' | '_creationTime'>;

export type PatchValue<Document extends GenericDocument> = Partial<WithoutSystemFields<Document>>;

export interface PaginationOptions {
  cursor: string | null;
  numItems: number;
}

export interface PaginationResult<TableName extends string, DataModel extends GenericDataModel> {
  page: DocumentByName<DataModel, TableName>[];
  isDone: boolean;
  continueCursor: string;
}

// A nested plain object we can descend into: excludes arrays, primitives, and
// branded strings (e.g. GenericId = string & { __table }) so paths stop at leaves.
type NestedRecord<T> = NonNullable<T> extends string
  ? never
  : NonNullable<T> extends any[]
    ? never
    : NonNullable<T> extends object
      ? NonNullable<T>
      : never;

// Recursively resolve the value type at a dotted field path, descending into
// nested plain object fields.
type FieldTypeAtPath<Doc, FieldPath extends string> = FieldPath extends `${infer First}.${infer Rest}`
  ? First extends keyof Doc
    ? NestedRecord<Doc[First]> extends infer Nested
      ? [Nested] extends [never]
        ? undefined
        : FieldTypeAtPath<Nested, Rest>
      : undefined
    : undefined
  : FieldPath extends keyof Doc
    ? Doc[FieldPath]
    : undefined;

export type FieldTypeFromFieldPath<
  Document extends GenericDocument,
  FieldPath extends string,
> = FieldTypeAtPath<Document, FieldPath> extends Value | undefined
  ? FieldTypeAtPath<Document, FieldPath>
  : Value | undefined;

// All field paths through a document, including dotted paths through nested
// plain object fields (arrays and branded strings are leaves).
export type FieldPaths<TableInfo extends { document: GenericDocument }> =
  NestedFieldPaths<TableInfo['document']>;

type NestedFieldPaths<Doc> = Doc extends string
  ? never
  : Doc extends any[]
    ? never
    : Doc extends object
      ? {
          [K in Extract<keyof Doc, string>]: NestedRecord<Doc[K]> extends never
            ? K
            : K | `${K}.${NestedFieldPaths<NestedRecord<Doc[K]>>}`;
        }[Extract<keyof Doc, string>]
      : never;

export abstract class Expression<T extends Value | undefined = Value | undefined> {
  private _value!: T;
}

export type ExpressionOrValue<T extends Value | undefined = Value | undefined> = Expression<T> | T;

export interface FilterBuilder<TableName extends string, DataModel extends GenericDataModel> {
  field<FieldPath extends FieldPaths<NamedTableInfo<DataModel, TableName>>>(
    fieldPath: FieldPath,
  ): Expression<FieldTypeFromFieldPath<DocumentByName<DataModel, TableName>, FieldPath>>;

  eq<T extends Value>(l: Expression<T>, r: ExpressionOrValue<T>): Expression<boolean>;
  eq<T extends Value>(l: ExpressionOrValue<T>, r: Expression<T>): Expression<boolean>;

  neq<T extends Value>(l: Expression<T>, r: ExpressionOrValue<T>): Expression<boolean>;
  neq<T extends Value>(l: ExpressionOrValue<T>, r: Expression<T>): Expression<boolean>;

  lt<T extends Value>(l: Expression<T>, r: ExpressionOrValue<T>): Expression<boolean>;
  lt<T extends Value>(l: ExpressionOrValue<T>, r: Expression<T>): Expression<boolean>;

  lte<T extends Value>(l: Expression<T>, r: ExpressionOrValue<T>): Expression<boolean>;
  lte<T extends Value>(l: ExpressionOrValue<T>, r: Expression<T>): Expression<boolean>;

  gt<T extends Value>(l: Expression<T>, r: ExpressionOrValue<T>): Expression<boolean>;
  gt<T extends Value>(l: ExpressionOrValue<T>, r: Expression<T>): Expression<boolean>;

  gte<T extends Value>(l: Expression<T>, r: ExpressionOrValue<T>): Expression<boolean>;
  gte<T extends Value>(l: ExpressionOrValue<T>, r: Expression<T>): Expression<boolean>;

  add<T extends NumericValue>(
    l: ExpressionOrValue<T>,
    r: ExpressionOrValue<T>,
  ): Expression<T>;
  sub<T extends NumericValue>(
    l: ExpressionOrValue<T>,
    r: ExpressionOrValue<T>,
  ): Expression<T>;
  mul<T extends NumericValue>(
    l: ExpressionOrValue<T>,
    r: ExpressionOrValue<T>,
  ): Expression<T>;
  div<T extends NumericValue>(
    l: ExpressionOrValue<T>,
    r: ExpressionOrValue<T>,
  ): Expression<T>;
  mod<T extends NumericValue>(
    l: ExpressionOrValue<T>,
    r: ExpressionOrValue<T>,
  ): Expression<T>;
  neg<T extends NumericValue>(x: ExpressionOrValue<T>): Expression<T>;

  and(...exprs: Array<Expression<boolean>>): Expression<boolean>;
  or(...exprs: Array<Expression<boolean>>): Expression<boolean>;
  not(x: Expression<boolean>): Expression<boolean>;
}

export type IndexNamesForTable<
  DataModel extends GenericDataModel,
  TableName extends TableNamesInDataModel<DataModel>,
> = NonNullable<NamedTableInfo<DataModel, TableName>['indexes']> extends never
  ? never
  : keyof NonNullable<NamedTableInfo<DataModel, TableName>['indexes']> & string;

export type NamedIndex<
  DataModel extends GenericDataModel,
  TableName extends TableNamesInDataModel<DataModel>,
  IndexName extends IndexNamesForTable<DataModel, TableName>,
> = NonNullable<NamedTableInfo<DataModel, TableName>['indexes']>[IndexName];

type PlusOne<N extends number> = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15][N];

export abstract class IndexRange {
  private _isIndexRange: undefined;
}

export interface UpperBoundIndexRangeBuilder<
  TableName extends string,
  DataModel extends GenericDataModel,
  IndexFieldName extends string,
> extends IndexRange {
  lt(
    fieldName: IndexFieldName,
    value: FieldTypeFromFieldPath<DocumentByName<DataModel, TableName>, IndexFieldName>,
  ): IndexRange;
  lte(
    fieldName: IndexFieldName,
    value: FieldTypeFromFieldPath<DocumentByName<DataModel, TableName>, IndexFieldName>,
  ): IndexRange;
}

export interface LowerBoundIndexRangeBuilder<
  TableName extends string,
  DataModel extends GenericDataModel,
  IndexFieldName extends string,
> extends UpperBoundIndexRangeBuilder<TableName, DataModel, IndexFieldName> {
  gt(
    fieldName: IndexFieldName,
    value: FieldTypeFromFieldPath<DocumentByName<DataModel, TableName>, IndexFieldName>,
  ): UpperBoundIndexRangeBuilder<TableName, DataModel, IndexFieldName>;
  gte(
    fieldName: IndexFieldName,
    value: FieldTypeFromFieldPath<DocumentByName<DataModel, TableName>, IndexFieldName>,
  ): UpperBoundIndexRangeBuilder<TableName, DataModel, IndexFieldName>;
}

export interface IndexRangeBuilder<
  TableName extends string,
  DataModel extends GenericDataModel,
  IndexFields extends readonly string[],
  FieldNum extends number = 0,
> extends LowerBoundIndexRangeBuilder<TableName, DataModel, IndexFields[FieldNum]> {
  eq(
    fieldName: IndexFields[FieldNum],
    value: FieldTypeFromFieldPath<DocumentByName<DataModel, TableName>, IndexFields[FieldNum]>,
  ): PlusOne<FieldNum> extends IndexFields['length']
    ? IndexRange
    : IndexRangeBuilder<TableName, DataModel, IndexFields, PlusOne<FieldNum>>;
}

export interface OrderedQuery<TableName extends string, DataModel extends GenericDataModel> {
  filter(
    predicate: (q: FilterBuilder<TableName, DataModel>) => Expression<boolean>,
  ): this;
  first(): Promise<DocumentByName<DataModel, TableName> | null>;
  unique(): Promise<DocumentByName<DataModel, TableName> | null>;
  take(n: number): Promise<DocumentByName<DataModel, TableName>[]>;
  collect(): Promise<DocumentByName<DataModel, TableName>[]>;
  paginate(opts: PaginationOptions): Promise<PaginationResult<TableName, DataModel>>;
}

export interface Query<TableName extends string, DataModel extends GenericDataModel> extends OrderedQuery<TableName, DataModel> {
  order(direction: 'asc' | 'desc'): OrderedQuery<TableName, DataModel>;
}

export interface QueryInitializer<TableName extends string, DataModel extends GenericDataModel> extends Query<TableName, DataModel> {
  withIndex<IndexName extends IndexNamesForTable<DataModel, TableName>>(
    indexName: IndexName,
    range?: (
      q: IndexRangeBuilder<
        TableName,
        DataModel,
        NamedIndex<DataModel, TableName, IndexName>['fields']
      >,
    ) => IndexRange,
  ): Query<TableName, DataModel>;
  fullTableScan(): Query<TableName, DataModel>;
}

export interface DatabaseReader<DataModel extends GenericDataModel> {
  get<TableName extends TableNamesInDataModel<DataModel>>(
    id: Id<TableName>,
  ): Promise<DocumentByName<DataModel, TableName> | null>;
  normalizeId<TableName extends TableNamesInDataModel<DataModel>>(
    table: TableName,
    id: string,
  ): Id<TableName> | null;
  query<TableName extends TableNamesInDataModel<DataModel>>(
    table: TableName,
  ): QueryInitializer<TableName, DataModel>;
}

export interface DatabaseWriter<DataModel extends GenericDataModel> extends DatabaseReader<DataModel> {
  insert<TableName extends TableNamesInDataModel<DataModel>>(
    table: TableName,
    value: InsertByName<DataModel, TableName>,
  ): Promise<Id<TableName>>;
  patch<TableName extends TableNamesInDataModel<DataModel>>(
    id: Id<TableName>,
    value: PatchValue<DocumentByName<DataModel, TableName>>,
  ): Promise<void>;
  replace<TableName extends TableNamesInDataModel<DataModel>>(
    id: Id<TableName>,
    value: InsertByName<DataModel, TableName>,
  ): Promise<void>;
  delete<TableName extends TableNamesInDataModel<DataModel>>(id: Id<TableName>): Promise<void>;
}

export interface UserIdentity {
  subject: string;
  tokenIdentifier: string;
  issuer: string;
  name?: string;
  givenName?: string;
  familyName?: string;
  nickname?: string;
  preferredUsername?: string;
  profileUrl?: string;
  pictureUrl?: string;
  email?: string;
  emailVerified?: boolean;
  gender?: string;
  birthday?: string;
  timezone?: string;
  language?: string;
  phoneNumber?: string;
  phoneNumberVerified?: boolean;
  updatedAt?: string;
  [claim: string]: any;
}

export interface AuthContext {
  getUserIdentity(): Promise<UserIdentity | null>;
}

declare const jobIdBrand: unique symbol;

export type JobId = string & { readonly [jobIdBrand]: 'JobId' };
export type JobID = JobId;

export type SchedulableFunctionReference<Args = any, Return = any> = FunctionReference<
  'mutation' | 'action',
  Args,
  Return,
  FunctionVisibility
>;

export interface SchedulerContext {
  runAfter<Ref extends SchedulableFunctionReference>(
    delayMs: number,
    func: Ref,
    ...args: OptionalRestArgs<Ref>
  ): Promise<JobId>;
  runAt<Ref extends SchedulableFunctionReference>(
    when: Date | number,
    func: Ref,
    ...args: OptionalRestArgs<Ref>
  ): Promise<JobId>;
  cancel(id: JobId): Promise<void>;
}

export interface CronJobDefinition {
  readonly name: string;
  readonly schedule: string;
  readonly functionName: string;
  readonly args: JSONValue;
}

export interface CronJobsDefinition {
  readonly kind: 'pbvex.cronJobs';
  readonly jobs: readonly CronJobDefinition[];
}

export class CronJobs implements CronJobsDefinition {
  readonly kind = 'pbvex.cronJobs' as const;
  private readonly definitions: CronJobDefinition[] = [];
  private readonly names = new Set<string>();

  get jobs(): readonly CronJobDefinition[] {
    return this.definitions;
  }

  cron<Ref extends SchedulableFunctionReference>(
    name: string,
    schedule: string,
    func: Ref,
    ...args: OptionalRestArgs<Ref>
  ): this {
    if (!/^[a-z][a-z0-9_-]{0,63}$/.test(name)) {
      throw new Error('Cron job name must match [a-z][a-z0-9_-]{0,63}');
    }
    if (this.names.has(name)) throw new Error(`Duplicate cron job name: ${name}`);
    if (this.definitions.length >= 64) throw new Error('A deployment can define at most 64 cron jobs');
    if (!isCronExpression(schedule)) throw new Error(`Invalid PocketBase cron expression: ${schedule}`);
    const reference = func as unknown as Record<string, unknown>;
    if (
      !reference ||
      typeof reference._path !== 'string' ||
      (reference._type !== 'mutation' && reference._type !== 'action')
    ) {
      throw new Error('Cron target must be a generated mutation or action reference');
    }
    const encodedArgs = encodeValue((args.length > 0 ? args[0] : {}) as PbvexValue);
    this.names.add(name);
    this.definitions.push(Object.freeze({
      name,
      schedule,
      functionName: reference._path,
      args: encodedArgs,
    }));
    return this;
  }
}

/** Creates the recurring job definition exported from `pbvex/crons.ts`. */
export function cronJobs(): CronJobs {
  return new CronJobs();
}

export function isCronJobsDefinition(value: unknown): value is CronJobsDefinition {
  if (!value || typeof value !== 'object') return false;
  const definition = value as Partial<CronJobsDefinition>;
  return definition.kind === 'pbvex.cronJobs' && Array.isArray(definition.jobs);
}

export interface StorageGetUrlOptions {
  /**
   * Identity-bound URLs are the default and require the caller's bearer token.
   * Capability URLs authorize anyone possessing the short-lived signed URL.
   * Public URLs are stable, CDN-cacheable bearer URLs that remain valid until deletion.
   */
  mode: 'identity' | 'capability' | 'public';
}

export interface StorageImageUploadOptions {
  /** Top-level schema table containing the image field. */
  table: string;
  /** Top-level field declared with `v.image()`. */
  field: string;
}

export interface StorageReader {
  getUrl: (id: StorageId, options?: StorageGetUrlOptions) => Promise<string | null>;
  getMetadata: (id: StorageId) => Promise<StorageFileMetadata | StorageImageMetadata | null>;
}

export interface StorageContext extends StorageReader {
  generateUploadUrl: (options?: StorageImageUploadOptions) => Promise<string>;
  delete: (id: StorageId) => Promise<void>;
}

export interface Headers {
  get(name: string): string | null;
  set(name: string, value: string): void;
  append(name: string, value: string): void;
  delete(name: string): void;
  has(name: string): boolean;
  forEach(callback: (value: string, name: string, headers: Headers) => void, thisArg?: any): void;
  entries(): IterableIterator<[string, string]>;
  keys(): IterableIterator<string>;
  values(): IterableIterator<string>;
  [Symbol.iterator](): IterableIterator<[string, string]>;
}

export const Headers: {
  new(init?: HeadersInit): Headers;
  prototype: Headers;
} = globalThis.Headers as any;

export type HeadersInit = Headers | Record<string, string> | string[][] | undefined;
export type BodyInit = string | Uint8Array | ArrayBuffer | null;
export type RequestInit = { method?: string; headers?: HeadersInit; body?: BodyInit };
export type ResponseInit = { status?: number; statusText?: string; headers?: HeadersInit };

export interface Request {
  readonly method: string;
  readonly url: string;
  readonly headers: Headers;
  readonly body: Uint8Array | null;
  readonly bodyUsed: boolean;
  text(): Promise<string>;
  json(): Promise<any>;
  arrayBuffer(): Promise<ArrayBuffer>;
}

export const Request: {
  new(input: string, init?: RequestInit): Request;
  prototype: Request;
} = globalThis.Request as any;

export interface Response {
  readonly status: number;
  readonly statusText: string;
  readonly headers: Headers;
  readonly body: Uint8Array | null;
  readonly bodyUsed: boolean;
  readonly ok: boolean;
  readonly type: string;
  readonly url: string;
  text(): Promise<string>;
  json(): Promise<any>;
  arrayBuffer(): Promise<ArrayBuffer>;
}

export const Response: {
  new(body?: BodyInit, init?: ResponseInit): Response;
  prototype: Response;
} = globalThis.Response as any;

export interface TextEncoder {
  encode(s: string): Uint8Array;
}

export const TextEncoder: {
  new(): TextEncoder;
  prototype: TextEncoder;
} = globalThis.TextEncoder as any;

export interface TextDecoder {
  decode(input?: ArrayBufferView | ArrayBuffer | null): string;
}

export const TextDecoder: {
  new(label?: string): TextDecoder;
  prototype: TextDecoder;
} = globalThis.TextDecoder as any;

export interface URLSearchParams {
  get(name: string): string | null;
  getAll(name: string): string[];
  set(name: string, value: string): void;
  append(name: string, value: string): void;
  delete(name: string): void;
  has(name: string): boolean;
  forEach(callback: (value: string, name: string, params: URLSearchParams) => void, thisArg?: any): void;
  entries(): IterableIterator<[string, string]>;
  keys(): IterableIterator<string>;
  values(): IterableIterator<string>;
  toString(): string;
  [Symbol.iterator](): IterableIterator<[string, string]>;
}

export const URLSearchParams: {
  new(init?: string | Record<string, string> | string[][] | URLSearchParams): URLSearchParams;
  prototype: URLSearchParams;
} = globalThis.URLSearchParams as any;

export interface URL {
  href: string;
  origin: string;
  protocol: string;
  username: string;
  password: string;
  host: string;
  hostname: string;
  port: string;
  pathname: string;
  search: string;
  hash: string;
  searchParams: URLSearchParams;
  toString(): string;
  toJSON(): string;
}

export const URL: {
  new(url: string, base?: string | URL): URL;
  prototype: URL;
} = globalThis.URL as any;

export interface HttpSendOptions {
  url: string;
  method?: 'GET' | 'HEAD' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  headers?: Readonly<Record<string, string>>;
  body?: string | Uint8Array | ArrayBuffer;
  timeoutMs?: number;
}

export interface HttpSendResponse {
  readonly statusCode: number;
  readonly headers: Readonly<Record<string, readonly string[]>>;
  readonly body: Uint8Array;
  readonly json: unknown | null;
}

export interface HttpContext {
  send(options: HttpSendOptions): Promise<HttpSendResponse>;
}

export interface FunctionContext<DataModel extends GenericDataModel = GenericDataModel> {
  auth?: AuthContext;
  db?: DatabaseReader<DataModel>;
  runQuery?<Ref extends FunctionReference<'query', any, any, any>>(
    ref: Ref,
    ...args: OptionalRestArgs<Ref>
  ): Promise<FunctionReturnType<Ref>>;
  runMutation?<Ref extends FunctionReference<'mutation', any, any, any>>(
    ref: Ref,
    ...args: OptionalRestArgs<Ref>
  ): Promise<FunctionReturnType<Ref>>;
  runAction?<Ref extends FunctionReference<'action', any, any, any>>(
    ref: Ref,
    ...args: OptionalRestArgs<Ref>
  ): Promise<FunctionReturnType<Ref>>;
  scheduler?: SchedulerContext;
  storage?: StorageReader;
  http?: HttpContext;
}

export type EmailTemplateVariables = Readonly<Record<string, string | number | boolean | null>>;
export interface EmailSendOptions<TemplateName extends string = string> {
  template: TemplateName;
  to: string | readonly string[];
  cc?: string | readonly string[];
  bcc?: string | readonly string[];
  variables?: EmailTemplateVariables;
}
export interface EmailContext<TemplateName extends string = string> {
  send(options: EmailSendOptions<TemplateName>): Promise<void>;
}

export interface QueryCtx<DataModel extends GenericDataModel = GenericDataModel> extends FunctionContext<DataModel> {
  auth: AuthContext;
  db: DatabaseReader<DataModel>;
  storage: StorageReader;
}

export interface MutationCtx<DataModel extends GenericDataModel = GenericDataModel> extends FunctionContext<DataModel> {
  auth: AuthContext;
  db: DatabaseWriter<DataModel>;
  scheduler: SchedulerContext;
  storage: StorageContext;
}

export interface ActionCtx<DataModel extends GenericDataModel = GenericDataModel, TemplateName extends string = string> extends FunctionContext<DataModel> {
  auth: AuthContext;
  db?: never;
  http: HttpContext;
  scheduler: SchedulerContext;
  email: EmailContext<TemplateName>;
  storage: StorageContext;
  runQuery<Ref extends FunctionReference<'query', any, any, any>>(
    ref: Ref,
    ...args: OptionalRestArgs<Ref>
  ): Promise<FunctionReturnType<Ref>>;
  runMutation<Ref extends FunctionReference<'mutation', any, any, any>>(
    ref: Ref,
    ...args: OptionalRestArgs<Ref>
  ): Promise<FunctionReturnType<Ref>>;
  runAction<Ref extends FunctionReference<'action', any, any, any>>(
    ref: Ref,
    ...args: OptionalRestArgs<Ref>
  ): Promise<FunctionReturnType<Ref>>;
  run<Ref extends FunctionReference<'query' | 'mutation' | 'action', any, any, any>>(
    ref: Ref,
    ...args: OptionalRestArgs<Ref>
  ): Promise<FunctionReturnType<Ref>>;
}

export interface HttpActionCtx<DataModel extends GenericDataModel = GenericDataModel, TemplateName extends string = string> extends ActionCtx<DataModel, TemplateName> {}

export interface FunctionDefinition<Args, Returns, Ctx> {
  type: FunctionType;
  visibility: Visibility;
  exportName: string;
  modulePath: string;
  name?: string;
  args: Validator<Args>;
  returns: Validator<Returns>;
  route?: FunctionRoute;
  handler: (ctx: Ctx, args: Args) => Returns | Promise<Returns>;
  /** @internal Component definition that owns this function, when applicable. */
  component?: TypedComponent<any, any, any>;
}

export interface QueryDef<Args, Returns, Ctx> extends FunctionDefinition<Args, Returns, Ctx> {
  type: 'query';
}

export interface MutationDef<Args, Returns, Ctx> extends FunctionDefinition<Args, Returns, Ctx> {
  type: 'mutation';
}

export interface ActionDef<Args, Returns, Ctx> extends FunctionDefinition<Args, Returns, Ctx> {
  type: 'action';
}

export interface HttpActionDef<Args, Returns, Ctx> extends FunctionDefinition<Args, Returns, Ctx> {
  type: 'httpAction';
}

export type HttpAction<
  Args extends Request = Request,
  Returns extends Response = Response,
  Ctx extends HttpActionCtx = HttpActionCtx,
> = HttpActionDef<Args, Returns, Ctx>;

export type ArgDefinition<Args> = Validator<Args> | { [K in keyof Args]: Validator<Args[K]> };
export type ReturnDefinition<Returns> = Validator<Returns> | { [K in keyof Returns]: Validator<Returns[K]> };

export interface FunctionOptions<Args, Returns, Ctx> {
  args?: ArgDefinition<Args> | undefined;
  returns?: ReturnDefinition<Returns> | undefined;
  route?: FunctionRoute;
  handler: (ctx: Ctx, args: Args) => Returns | Promise<Returns>;
}

export { isValidator } from './values.js';

export function isFunctionDefinition(value: unknown): value is FunctionDefinition<any, any, any> {
  if (value === null || typeof value !== 'object') return false;
  const obj = value as Record<string, unknown>;
  return ['query', 'mutation', 'action', 'httpAction'].includes(obj.type as string) && typeof obj.handler === 'function';
}

export function normalizeArgs<Args>(args: ArgDefinition<Args> | undefined): Validator<Args> {
  if (args === undefined) {
    return v.object({}) as unknown as Validator<Args>;
  }
  if (isValidator<Args>(args)) {
    return args;
  }
  return v.object(args as Record<string, Validator<any>>) as unknown as Validator<Args>;
}

export function normalizeReturns<Returns>(returns: ReturnDefinition<Returns> | undefined): Validator<Returns> {
  if (returns === undefined) {
    return v.any() as unknown as Validator<Returns>;
  }
  if (isValidator<Returns>(returns)) {
    return returns;
  }
  return v.object(returns as Record<string, Validator<any>>) as unknown as Validator<Returns>;
}

function createFunction<Args, Returns, Ctx>(
  type: FunctionType,
  visibility: Visibility,
  options: FunctionOptions<Args, Returns, Ctx>,
  component?: TypedComponent<any, any, any>,
): FunctionDefinition<Args, Returns, Ctx> {
  const args = normalizeArgs(options.args);
  const returns = normalizeReturns(options.returns);
  return {
    type,
    visibility,
    exportName: '',
    modulePath: '',
    args,
    returns,
    route: options.route,
    handler: options.handler,
    component,
  };
}

export function query<Args = Record<string, never>, Returns = any, DataModel extends GenericDataModel = GenericDataModel>(
  options: FunctionOptions<Args, Returns, QueryCtx<DataModel>>,
): QueryDef<Args, Returns, QueryCtx<DataModel>> {
  return createFunction('query', 'public', options) as QueryDef<Args, Returns, QueryCtx<DataModel>>;
}

export function internalQuery<Args = Record<string, never>, Returns = any, DataModel extends GenericDataModel = GenericDataModel>(
  options: FunctionOptions<Args, Returns, QueryCtx<DataModel>>,
): QueryDef<Args, Returns, QueryCtx<DataModel>> {
  return createFunction('query', 'internal', options) as QueryDef<Args, Returns, QueryCtx<DataModel>>;
}

export function mutation<Args = Record<string, never>, Returns = any, DataModel extends GenericDataModel = GenericDataModel>(
  options: FunctionOptions<Args, Returns, MutationCtx<DataModel>>,
): MutationDef<Args, Returns, MutationCtx<DataModel>> {
  return createFunction('mutation', 'public', options) as MutationDef<Args, Returns, MutationCtx<DataModel>>;
}

export function internalMutation<Args = Record<string, never>, Returns = any, DataModel extends GenericDataModel = GenericDataModel>(
  options: FunctionOptions<Args, Returns, MutationCtx<DataModel>>,
): MutationDef<Args, Returns, MutationCtx<DataModel>> {
  return createFunction('mutation', 'internal', options) as MutationDef<Args, Returns, MutationCtx<DataModel>>;
}

export function action<Args = Record<string, never>, Returns = any, DataModel extends GenericDataModel = GenericDataModel>(
  options: FunctionOptions<Args, Returns, ActionCtx<DataModel>>,
): ActionDef<Args, Returns, ActionCtx<DataModel>> {
  return createFunction('action', 'public', options) as ActionDef<Args, Returns, ActionCtx<DataModel>>;
}

export function internalAction<Args = Record<string, never>, Returns = any, DataModel extends GenericDataModel = GenericDataModel>(
  options: FunctionOptions<Args, Returns, ActionCtx<DataModel>>,
): ActionDef<Args, Returns, ActionCtx<DataModel>> {
  return createFunction('action', 'internal', options) as ActionDef<Args, Returns, ActionCtx<DataModel>>;
}

export function httpAction<Args extends Request = Request, Returns extends Response = Response, DataModel extends GenericDataModel = GenericDataModel>(
  options: FunctionOptions<Args, Returns, HttpActionCtx<DataModel>>,
): HttpActionDef<Args, Returns, HttpActionCtx<DataModel>> {
  return createFunction('httpAction', 'public', options) as HttpActionDef<Args, Returns, HttpActionCtx<DataModel>>;
}

type ComponentArgs<C> = C extends TypedComponent<infer Args, any, any> ? Args : never;
type ComponentEnv<C> = C extends TypedComponent<any, infer Env, any> ? ResolveEnv<Env> : never;
type ComponentCtx<C, Base> = Base & { readonly args: ComponentArgs<C>; readonly env: ComponentEnv<C> };

/** Creates function factories whose contexts are typed from a component definition. */
export function defineComponentFns<C extends TypedComponent<any, any, any>>(component: C) {
  return {
    query: <Returns = any>(options: FunctionOptions<Record<string, never>, Returns, ComponentCtx<C, QueryCtx>>) =>
      createFunction('query', 'public', options, component),
    internalQuery: <Returns = any>(options: FunctionOptions<Record<string, never>, Returns, ComponentCtx<C, QueryCtx>>) =>
      createFunction('query', 'internal', options, component),
    mutation: <Returns = any>(options: FunctionOptions<Record<string, never>, Returns, ComponentCtx<C, MutationCtx>>) =>
      createFunction('mutation', 'public', options, component),
    internalMutation: <Returns = any>(options: FunctionOptions<Record<string, never>, Returns, ComponentCtx<C, MutationCtx>>) =>
      createFunction('mutation', 'internal', options, component),
    action: <Returns = any>(options: FunctionOptions<Record<string, never>, Returns, ComponentCtx<C, ActionCtx>>) =>
      createFunction('action', 'public', options, component),
    internalAction: <Returns = any>(options: FunctionOptions<Record<string, never>, Returns, ComponentCtx<C, ActionCtx>>) =>
      createFunction('action', 'internal', options, component),
  };
}
