export type JSONValue =
  | null
  | boolean
  | number
  | string
  | JSONValue[]
  | { [key: string]: JSONValue };

export type JSONObject = { [key: string]: JSONValue };

export type DeploymentProtocolVersion = 'v1';

export type FunctionType = 'query' | 'mutation' | 'action' | 'httpAction';
export type FunctionVisibility = 'public' | 'internal';

export type FunctionRoute = Readonly<{
  method: string;
  path?: string;
  pathPrefix?: string;
}>;

/**
 * A shared reference to a registered PBVex function.
 *
 * @typeParam Type - The function kind (`query`, `mutation`, `action`, `httpAction`).
 * @typeParam Args - The (object) arguments to the function.
 * @typeParam Return - The return type of the function.
 * @typeParam Visibility - Whether the function is `public` or `internal`.
 */
export type FunctionReference<
  Type extends FunctionType,
  Args = any,
  Return = any,
  Visibility extends FunctionVisibility = 'public',
> = {
  _path: string;
  _type: Type;
  _visibility: Visibility;
  __args?: Args;
  __return?: Return;
} & NoArgsDiscriminator<Args>;

/**
 * Runtime discriminator for no-args/empty-args references. References whose
 * Args are void/undefined/empty-object MUST carry `__noArgs: true` (codegen
 * emits it); references with real args MUST NOT carry it. The client uses this
 * to disambiguate the single-slot case (options vs args) without guessing from
 * the value shape, so an args object shaped like { timeoutMs } is never
 * misclassified as options.
 */
type NoArgsDiscriminator<Args> = IsAny<Args> extends true
  ? { __noArgs?: true } // any-args params accept references with or without the flag
  : IsEmptyArgs<Args> extends true
    ? { __noArgs: true }
    : { __noArgs?: never };

export type FunctionArgs<FuncRef extends FunctionReference<any, any, any, any>> =
  FuncRef extends FunctionReference<any, infer Args, any, any> ? Args : never;

export type FunctionReturnType<FuncRef extends FunctionReference<any, any, any, any>> =
  FuncRef extends FunctionReference<any, any, infer Return, any> ? Return : never;

export type EmptyObject = Record<string, never>;

type IsAny<T> = 0 extends (1 & T) ? true : false;

// True when a reference takes no caller-supplied args (void/undefined/empty
// object). `any` is excluded so untyped references keep slot 0 as args.
type IsEmptyArgs<Args> = IsAny<Args> extends true
  ? false
  : [Args] extends [undefined | void | EmptyObject]
    ? true
    : false;

/**
 * Tuple of arguments for a FunctionReference suitable for a rest parameter.
 *
 * - void/undefined args take no slot.
 * - Empty/all-optional object args may be omitted or supplied as an object.
 * - Required args must be supplied exactly once.
 */
export type OptionalRestArgs<FuncRef extends FunctionReference<any, any, any, any>> =
  IsAny<FunctionArgs<FuncRef>> extends true
    ? [args?: any]
    : [FunctionArgs<FuncRef>] extends [undefined | void]
      ? []
      : FunctionArgs<FuncRef> extends EmptyObject
        ? [args?: EmptyObject]
        : {} extends FunctionArgs<FuncRef>
          ? [args?: FunctionArgs<FuncRef>]
          : [args: FunctionArgs<FuncRef>];

/**
 * Tuple of function args and call options for typed overloads.
 */
export type ArgsAndOptions<
  FuncRef extends FunctionReference<any, any, any, any>,
  Options = Record<string, never>,
> = IsAny<FunctionArgs<FuncRef>> extends true
  ? [args?: any, options?: Options]
  : IsEmptyArgs<FunctionArgs<FuncRef>> extends true
    ? [options?: Options]
    : {} extends FunctionArgs<FuncRef>
       ? [args?: FunctionArgs<FuncRef>, options?: Options]
       : [args: FunctionArgs<FuncRef>, options?: Options];

export type FunctionDescriptor = Readonly<{
  name: string;
  type: FunctionType;
  visibility: FunctionVisibility;
  modulePath: string;
  exportName: 'default' | string;
  args?: JSONValue;
  returns?: JSONValue;
  route?: FunctionRoute;
}>;

export type DeploymentConfig = Readonly<{
  httpPathPrefix: string;
  realtimePath: string;
  maxUploadBytes: number;
  maxFunctionArgsBytes: number;
  maxReturnValueBytes: number;
  defaultRequestTimeoutMs: number;
}>;

export type SchemaDescriptor = Readonly<{
  tables: TableDescriptor[];
}>;

export type TableDescriptor = Readonly<{
  tableName: string;
  fields: Record<string, JSONValue>;
  indexes?: IndexDescriptor[];
}>;

export type IndexDescriptor = Readonly<{
  name: string;
  fields: string[];
}>;

export type ValidatorDescriptor = JSONValue;

export type MigrationDescriptor = Readonly<{
  id: string;
  table: string;
  mode: 'transactional';
  from: ValidatorDescriptor;
  to: ValidatorDescriptor;
  sourceSchemaHash: string;
  targetSchemaHash: string;
  checksum: string;
  modulePath: string;
  exportName: 'default' | string;
  reversibility: 'reversible';
}>;

export type EnvArgDescriptor = Readonly<{
  type: 'value' | 'envVar';
  value?: string;
  name?: string;
}>;

export type ComponentDefinition = Readonly<{
  componentId: string;
  modulePaths: string[];
  moduleHashes?: Record<string, string>;
  args?: ValidatorDescriptor;
  schema?: SchemaDescriptor;
  env?: Record<string, EnvArgDescriptor>;
  dependencies?: string[];
}>;

export type ComponentMount = Readonly<{
  name: string;
  componentId: string;
  args?: JSONValue;
  children?: ComponentMount[];
}>;

export type ComponentGraph = Readonly<{
  definitions: ComponentDefinition[];
  mounts: ComponentMount[];
}>;

export type DeploymentManifest = Readonly<{
  protocolVersion: DeploymentProtocolVersion;
  deploymentId: string;
  functions?: FunctionDescriptor[];
  components?: ComponentGraph;
  config?: Partial<DeploymentConfig>;
  schema?: SchemaDescriptor;
  emailTemplates?: EmailTemplateManifest;
  cronJobs?: CronJobDescriptor[];
  migrations?: MigrationDescriptor[];
}>;

export type CronJobDescriptor = Readonly<{
  name: string;
  schedule: string;
  functionName: string;
  args: JSONValue;
}>;

export type EmailTemplate = Readonly<{ name: string; subject: string; text?: string; html?: string }>;
export type EmailTemplateManifest = Readonly<{ sha256: string; entries: EmailTemplate[] }>;

export type DeploymentBundle = Readonly<{
  js: string;
  sha256: string;
  size: number;
}>;

export type Deployment = Readonly<{
  deploymentId: string;
  manifest: DeploymentManifest;
  bundle: DeploymentBundle;
  createdAt: string;
  activatedAt?: string;
  active: boolean;
}>;

export type ModuleSource = Readonly<{
  /** Full module path (e.g. pbvex/components/counter/store.ts). */
  path: string;
  /** Base64-encoded UTF-8 module source bytes. */
  bytes: string;
}>;

export type DeploymentUploadRequest = Readonly<{
  manifest: DeploymentManifest;
  bundle: string;
  sha256: string;
  size: number;
  /** Uploaded module sources used to authenticate declared moduleHashes. */
  modules?: ModuleSource[];
}>;

export type DeploymentUploadResponse = Readonly<{
  deploymentId: string;
  bundleHash: string;
  acceptedAt: string;
}>;

export type DeploymentListResponse = Readonly<{
  deployments: Deployment[];
}>;

export type DeploymentActivateRequest = Readonly<{
  atomic: boolean;
}>;

export type MigrationWarning = Readonly<{
  code: 'transactional_migration_utilization';
  rows: number;
  rowLimit: number;
  estimatedBytes: number;
  byteLimit: number;
  utilizationPercent: number;
}>;

export type DeploymentActivateResponse = Readonly<{
  deploymentId: string;
  activatedAt: string;
  previousDeploymentId?: string;
  warnings?: MigrationWarning[];
}>;

export type DeploymentRollbackResponse = Readonly<{
  deploymentId: string;
  rolledBackAt: string;
  restoredDeploymentId?: string;
}>;

export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';

export type HttpEnvelope = Readonly<{
  method: HttpMethod;
  path: string;
  headers?: Record<string, string>;
  query?: Record<string, string | string[]>;
  body?: JSONValue;
}>;

export type HttpResponse = Readonly<{
  status: number;
  headers?: Record<string, string>;
  body: JSONValue | null;
}>;

export type RealtimeOp = 'message' | 'subscribe' | 'unsubscribe' | 'ping' | 'pong';

export type RealtimeEnvelope = Readonly<{
  id: string;
  op: RealtimeOp;
  path?: string;
  args?: JSONValue;
  payload?: JSONValue;
  /** Server-advertised maximum SSE event-data size in bytes. */
  maxEventSize?: number;
}>;

export type SseEnvelope = Readonly<{
  event?: string;
  id?: string;
  data: RealtimeEnvelope;
}>;

export type ErrorCode =
  | 'bad_request'
  | 'invalid_manifest'
  | 'invalid_function'
  | 'bundle_not_found'
  | 'bundle_hash_mismatch'
  | 'activation_failed'
  | 'not_found'
  | 'unauthorized'
  | 'forbidden'
  | 'conflict'
  | 'upload_expired'
  | 'upload_consumed'
  | 'upload_pending'
  | 'upload_too_large'
  | 'invalid_content'
  | 'storage_full'
  | 'internal';

export const ERROR_CODES = [
  'bad_request',
  'invalid_manifest',
  'invalid_function',
  'bundle_not_found',
  'bundle_hash_mismatch',
  'activation_failed',
  'not_found',
  'unauthorized',
  'forbidden',
  'conflict',
  'upload_expired',
  'upload_consumed',
  'upload_pending',
  'upload_too_large',
  'invalid_content',
  'storage_full',
  'internal',
] as const satisfies readonly ErrorCode[];

declare const storageIdBrand: unique symbol;

/** Opaque identifier returned by the PBVex storage upload endpoint. */
export type StorageId = string & { readonly [storageIdBrand]: 'StorageId' };

export type StorageFileMetadata = Readonly<{
  storageId: StorageId;
  kind: 'file';
  /** Token identifier that requested the upload URL, or an empty string for anonymous issuance. */
  createdBy: string;
  filename: string;
  contentType: string;
  extension: string;
  size: number;
  sha256: string;
}>;

export type StorageImageMetadata = Omit<StorageFileMetadata, 'kind'> & Readonly<{
  kind: 'image';
  width: number;
  height: number;
  thumbs: readonly string[];
}>;

export type StorageUploadResponse = Readonly<{
  storageId: StorageId;
  metadata?: StorageFileMetadata | StorageImageMetadata;
}>;

export type StructuredError = Readonly<{
  error: true;
  code: ErrorCode;
  message: string;
  details?: unknown[];
  /** Wire-safe application data supplied by an application error. */
  data?: JSONValue;
  requestId?: string;
}>;
