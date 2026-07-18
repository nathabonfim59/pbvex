import type {
  DeploymentConfig,
  DeploymentManifest,
  DeploymentBundle,
  Deployment,
  DeploymentUploadRequest,
  DeploymentUploadResponse,
  DeploymentActivateRequest,
  DeploymentActivateResponse,
  DeploymentRollbackResponse,
  DeploymentListResponse,
  FunctionDescriptor,
  SchemaDescriptor,
  TableDescriptor,
  IndexDescriptor,
  MigrationDescriptor,
  MigrationWarning,
  JSONValue,
} from './types.js';
import {
  isIdentifier,
  isModulePath,
  isFunctionType,
  isFunctionVisibility,
  isExportName,
  isSha256Hex,
  isNonNegativeInteger,
  isJsonValue,
  isBase64String,
  isSafeFieldName,
  isIsoDateString,
  MAX_VALUE_DEPTH,
} from './validators.js';
import { isHttpMethod } from './http.js';
import { canonicalHash, canonicalHashSync, hashSha256Bytes } from './canonical.js';
import { parseOpaqueId } from './ids.js';
import { isCronExpression } from './cron.js';
import {
  authenticateComponentIds,
  authenticateComponentIdsSync,
  validateComponentFunctionBinding,
  validateComponents,
  validateModuleSources,
  verifyModuleSources,
} from './components.js';
import {
  DEFAULT_CONFIG,
  MAX_DEPLOYMENT_UPLOAD_BYTES,
  MAX_FUNCTION_ARGS_LIMIT,
  MAX_RETURN_VALUE_LIMIT,
} from './config.js';

export {
  DEFAULT_CONFIG,
  MAX_DEPLOYMENT_UPLOAD_BYTES,
  MAX_FUNCTION_ARGS_LIMIT,
  MAX_RETURN_VALUE_LIMIT,
} from './config.js';

export const MAX_UNION_BRANCHES = 64;
export const MAX_MAP_SIZE = 1024;
export const MAX_ARRAY_SIZE = 1024;
export const MAX_BUDGET_NODES = 16 * 1024;
export const MAX_BUDGET_BYTES = 4 << 20;
export const MAX_ACTIVATION_WARNINGS = 1;
const MIGRATION_WARNING_ROW_LIMIT = 10_000;
const MIGRATION_WARNING_BYTE_LIMIT = 64 << 20;

export function normalizeConfig(config?: Partial<DeploymentConfig>): DeploymentConfig {
  return {
    httpPathPrefix: config?.httpPathPrefix ?? DEFAULT_CONFIG.httpPathPrefix,
    realtimePath: config?.realtimePath ?? DEFAULT_CONFIG.realtimePath,
    maxUploadBytes: config?.maxUploadBytes ?? DEFAULT_CONFIG.maxUploadBytes,
    maxFunctionArgsBytes: config?.maxFunctionArgsBytes ?? DEFAULT_CONFIG.maxFunctionArgsBytes,
    maxReturnValueBytes: config?.maxReturnValueBytes ?? DEFAULT_CONFIG.maxReturnValueBytes,
    defaultRequestTimeoutMs: config?.defaultRequestTimeoutMs ?? DEFAULT_CONFIG.defaultRequestTimeoutMs,
  };
}

export function validateManifest(value: unknown): DeploymentManifest {
  if (typeof value !== 'object' || value === null) {
    throw new Error('Manifest must be a JSON object');
  }
  const o = value as Record<string, unknown>;
  if (o.protocolVersion !== 'v1') throw new Error('protocolVersion must be "v1"');
  if (!isIdentifier(o.deploymentId)) throw new Error('Invalid deploymentId');
  const functions = validateFunctions(o.functions);
  const components = validateComponents(o.components);
  validateComponentFunctionBinding(functions ?? [], components);
  for (const definition of components?.definitions ?? []) {
    if (definition.schema !== undefined) validateSchema(definition.schema);
  }
  const config = validateConfig(o.config);
  const schema = 'schema' in o && o.schema !== undefined ? validateSchema(o.schema) : undefined;
  const emailTemplates = validateEmailTemplates(o.emailTemplates);
  const cronJobs = validateCronJobs(o.cronJobs, functions ?? [], normalizeConfig(config));
  const migrations = validateMigrations(o.migrations, schema);
  return {
    protocolVersion: 'v1',
    deploymentId: o.deploymentId,
    functions,
    components,
    config,
    schema,
    emailTemplates,
    cronJobs,
    migrations,
  };
}

const MIGRATION_KEYS = [
  'id', 'table', 'mode', 'from', 'to', 'sourceSchemaHash', 'targetSchemaHash',
  'checksum', 'modulePath', 'exportName', 'reversibility',
];

function validateMigrations(value: unknown, targetSchema?: SchemaDescriptor): MigrationDescriptor[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.length > 256) {
    throw new Error('migrations must be an array with at most 256 entries');
  }
  const ids = new Set<string>();
  const tables = new Set(targetSchema?.tables.map((table) => table.tableName) ?? []);
  const entries = value.map((raw, index): MigrationDescriptor => {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
      throw new Error(`Migration[${index}] must be an object`);
    }
    const migration = raw as Record<string, unknown>;
    if (Object.keys(migration).some((key) => !MIGRATION_KEYS.includes(key))) {
      throw new Error(`Migration[${index}] has unknown fields`);
    }
    if (typeof migration.id !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(migration.id)) {
      throw new Error(`Migration[${index}] id is invalid`);
    }
    if (ids.has(migration.id)) throw new Error(`Migration[${index}] id is duplicated`);
    if (!isIdentifier(migration.table) || migration.table.toLowerCase().startsWith('pbvex_cmp_') || !tables.has(migration.table)) {
      throw new Error(`Migration[${index}] table is invalid`);
    }
    if (migration.mode !== 'transactional') throw new Error(`Migration[${index}] mode is invalid`);
    if (!isObjectValidatorDescriptor(migration.from)) throw new Error(`Migration[${index}] from must be an object validator`);
    if (!isObjectValidatorDescriptor(migration.to)) throw new Error(`Migration[${index}] to must be an object validator`);
    if (!isSha256Hex(migration.sourceSchemaHash)) throw new Error(`Migration[${index}] sourceSchemaHash is invalid`);
    if (!isSha256Hex(migration.targetSchemaHash)) throw new Error(`Migration[${index}] targetSchemaHash is invalid`);
    if (canonicalHashSync(migration.from as JSONValue) !== migration.sourceSchemaHash) {
      throw new Error(`Migration[${index}] sourceSchemaHash does not match from`);
    }
    if (canonicalHashSync(migration.to as JSONValue) !== migration.targetSchemaHash) {
      throw new Error(`Migration[${index}] targetSchemaHash does not match to`);
    }
    if (!isSha256Hex(migration.checksum)) throw new Error(`Migration[${index}] checksum is invalid`);
    if (
      !isModulePath(migration.modulePath) ||
      !migration.modulePath.startsWith('pbvex/migrations/') ||
      !migration.modulePath.endsWith('.ts')
    ) throw new Error(`Migration[${index}] modulePath is invalid`);
    if (!isExportName(migration.exportName)) throw new Error(`Migration[${index}] exportName is invalid`);
    if (migration.reversibility !== 'reversible') throw new Error(`Migration[${index}] reversibility is invalid`);
    ids.add(migration.id);
    return {
      id: migration.id,
      table: migration.table,
      mode: 'transactional',
      from: migration.from as JSONValue,
      to: migration.to as JSONValue,
      sourceSchemaHash: migration.sourceSchemaHash,
      targetSchemaHash: migration.targetSchemaHash,
      checksum: migration.checksum,
      modulePath: migration.modulePath,
      exportName: migration.exportName,
      reversibility: 'reversible',
    };
  });
  for (let index = 1; index < entries.length; index += 1) {
    if (entries[index - 1]!.id >= entries[index]!.id) {
      throw new Error('migrations entries must be sorted by id');
    }
  }
  return entries;
}

function isObjectValidatorDescriptor(value: unknown): boolean {
  return isValidValidatorDescriptor(value) && (value as Record<string, unknown>).type === 'object';
}

function validateCronJobs(
  value: unknown,
  functions: FunctionDescriptor[],
  config: DeploymentConfig,
): DeploymentManifest['cronJobs'] {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.length > 64) throw new Error('cronJobs must be an array with at most 64 entries');
  const functionByName = new Map(functions.map((fn) => [fn.name, fn]));
  const names = new Set<string>();
  const entries = value.map((raw, index) => {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error(`Cron job[${index}] must be an object`);
    const job = raw as Record<string, unknown>;
    if (Object.keys(job).some((key) => !['name', 'schedule', 'functionName', 'args'].includes(key))) {
      throw new Error(`Cron job[${index}] has unknown fields`);
    }
    if (typeof job.name !== 'string' || !/^[a-z][a-z0-9_-]{0,63}$/.test(job.name) || names.has(job.name)) {
      throw new Error(`Cron job[${index}] name is invalid or duplicated`);
    }
    if (!isCronExpression(job.schedule)) throw new Error(`Cron job[${index}] schedule is invalid`);
    if (typeof job.functionName !== 'string') throw new Error(`Cron job[${index}] functionName is invalid`);
    const target = functionByName.get(job.functionName);
    if (!target || (target.type !== 'mutation' && target.type !== 'action')) {
      throw new Error(`Cron job[${index}] target must be a deployed mutation or action`);
    }
    if (!isJsonValue(job.args)) throw new Error(`Cron job[${index}] args must be a valid PBVex wire value`);
    if (new TextEncoder().encode(JSON.stringify(job.args)).byteLength > config.maxFunctionArgsBytes) {
      throw new Error(`Cron job[${index}] args exceed maxFunctionArgsBytes`);
    }
    names.add(job.name);
    return { name: job.name, schedule: job.schedule, functionName: job.functionName, args: job.args };
  });
  for (let index = 1; index < entries.length; index += 1) {
    if (entries[index - 1]!.name >= entries[index]!.name) throw new Error('cronJobs entries must be sorted by name');
  }
  return entries;
}

export async function validateUploadRequest(value: unknown): Promise<DeploymentUploadRequest> {
  if (typeof value !== 'object' || value === null) throw new Error('Upload request must be an object');
  const o = value as Record<string, unknown>;
  const manifest = validateManifest(o.manifest);
  if (typeof o.bundle !== 'string') throw new Error('bundle must be a string');
  if (!isBase64String(o.bundle)) throw new Error('bundle must be a valid base64 string');
  if (!isSha256Hex(o.sha256)) throw new Error('sha256 must be a lowercase hex SHA-256');
  if (!isNonNegativeInteger(o.size)) throw new Error('size must be a non-negative integer');
  const bytes = base64ToArrayBuffer(o.bundle);
  if (bytes.byteLength !== o.size) {
    throw new Error(`size mismatch: expected ${bytes.byteLength} bytes`);
  }
  const hash = await hashSha256Bytes(bytes);
  if (hash !== o.sha256) throw new Error('sha256 does not match bundle bytes');
  if (manifest.emailTemplates && await canonicalHash({ bundleSha256: o.sha256, entries: manifest.emailTemplates.entries }) !== manifest.emailTemplates.sha256) {
    throw new Error('emailTemplates hash does not match bundle and entries');
  }
  const modules = validateModuleSources(o.modules);
  if (manifest.components) {
    if (!modules?.length) throw new Error('modules are required when components are declared');
    await verifyModuleSources(modules, manifest.components);
    await authenticateComponentIds(manifest.components, o.sha256);
  }
  return {
    manifest,
    bundle: o.bundle,
    sha256: o.sha256,
    size: o.size,
    modules,
  };
}

function validateEmailTemplates(value: unknown): DeploymentManifest['emailTemplates'] {
  if (value === undefined) return undefined;
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('emailTemplates must be an object');
  const o = value as Record<string, unknown>;
  if (Object.keys(o).some((key) => key !== 'sha256' && key !== 'entries')) throw new Error('unknown emailTemplates field');
  if (!isSha256Hex(o.sha256) || !Array.isArray(o.entries) || o.entries.length > 64) throw new Error('invalid emailTemplates manifest');
  let total = 0;
  const names = new Set<string>();
  const entries = o.entries.map((raw) => {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('invalid email template');
    const t = raw as Record<string, unknown>;
    if (Object.keys(t).some((key) => !['name', 'subject', 'text', 'html'].includes(key))) throw new Error('unknown email template field');
    if (typeof t.name !== 'string' || !/^[a-z][a-z0-9_-]{0,63}$/.test(t.name) || names.has(t.name)) throw new Error('invalid or duplicate email template name');
    if (typeof t.subject !== 'string' || t.subject.length === 0 || new TextEncoder().encode(t.subject).byteLength > 998 || /[\r\n]/.test(t.subject)) throw new Error('invalid email template subject');
    if ((t.text !== undefined && (typeof t.text !== 'string' || t.text.length === 0)) || (t.html !== undefined && (typeof t.html !== 'string' || t.html.length === 0)) || (t.text === undefined && t.html === undefined)) throw new Error('email template needs non-empty text or html');
    names.add(t.name); total += new TextEncoder().encode(t.subject + ((t.text as string | undefined) ?? '') + ((t.html as string | undefined) ?? '')).byteLength;
    if (total > 512 * 1024) throw new Error('email templates exceed size limit');
    return { name: t.name, subject: t.subject, ...(t.text ? { text: t.text as string } : {}), ...(t.html ? { html: t.html as string } : {}) };
  });
  for (let i = 1; i < entries.length; i++) if (entries[i - 1]!.name >= entries[i]!.name) throw new Error('email template entries must be sorted');
  return { sha256: o.sha256, entries };
}

export function validateActivateRequest(value: unknown): DeploymentActivateRequest {
  if (typeof value !== 'object' || value === null) throw new Error('Activate request must be an object');
  const o = value as Record<string, unknown>;
  if (typeof o.atomic !== 'boolean') throw new Error('atomic must be a boolean');
  return { atomic: o.atomic };
}

export function validateDeployment(value: unknown): Deployment {
  if (typeof value !== 'object' || value === null) throw new Error('Deployment must be an object');
  const o = value as Record<string, unknown>;
  if (!isIdentifier(o.deploymentId)) throw new Error('Invalid deploymentId');
  const manifest = validateManifest(o.manifest);
  const bundle = validateDeploymentBundle(o.bundle);
  authenticateComponentIdsSync(manifest.components, bundle.sha256);
  if (manifest.emailTemplates && canonicalHashSync({ bundleSha256: bundle.sha256, entries: manifest.emailTemplates.entries }) !== manifest.emailTemplates.sha256) {
    throw new Error('emailTemplates hash does not match bundle and entries');
  }
  if (typeof o.createdAt !== 'string') throw new Error('createdAt must be an ISO timestamp string');
  if (typeof o.active !== 'boolean') throw new Error('active must be a boolean');
  const activatedAt = o.activatedAt;
  if (activatedAt !== undefined && typeof activatedAt !== 'string') {
    throw new Error('activatedAt must be a string');
  }
  return {
    deploymentId: o.deploymentId,
    manifest,
    bundle,
    createdAt: o.createdAt,
    activatedAt,
    active: o.active,
  };
}

export function validateDeploymentListResponse(value: unknown): DeploymentListResponse {
  if (typeof value !== 'object' || value === null) throw new Error('Deployment list response must be an object');
  const o = value as Record<string, unknown>;
  if (!Array.isArray(o.deployments)) throw new Error('deployments must be an array');
  return { deployments: o.deployments.map(validateDeployment) };
}

export function validateUploadResponse(value: unknown): DeploymentUploadResponse {
  if (typeof value !== 'object' || value === null) throw new Error('Upload response must be an object');
  const o = value as Record<string, unknown>;
  if (!isIdentifier(o.deploymentId)) throw new Error('deploymentId is invalid');
  if (!isSha256Hex(o.bundleHash)) throw new Error('bundleHash must be a hex SHA-256');
  if (!isIsoDateString(o.acceptedAt)) throw new Error('acceptedAt must be an ISO date string');
  return { deploymentId: o.deploymentId, bundleHash: o.bundleHash, acceptedAt: o.acceptedAt };
}

export function validateActivateResponse(value: unknown): DeploymentActivateResponse {
  if (typeof value !== 'object' || value === null) throw new Error('Activate response must be an object');
  const o = value as Record<string, unknown>;
  if (!isIdentifier(o.deploymentId)) throw new Error('deploymentId is invalid');
  if (!isIsoDateString(o.activatedAt)) throw new Error('activatedAt must be an ISO date string');
  const previousDeploymentId = 'previousDeploymentId' in o && o.previousDeploymentId !== undefined ? o.previousDeploymentId : undefined;
  if (previousDeploymentId !== undefined && !isIdentifier(previousDeploymentId)) throw new Error('previousDeploymentId is invalid');
  const warnings = validateMigrationWarnings(o.warnings);
  return { deploymentId: o.deploymentId, activatedAt: o.activatedAt, previousDeploymentId: previousDeploymentId as string | undefined, warnings };
}

function validateMigrationWarnings(value: unknown): MigrationWarning[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.length > MAX_ACTIVATION_WARNINGS) {
    throw new Error(`warnings must be an array with at most ${MAX_ACTIVATION_WARNINGS} entries`);
  }
  const keys = ['code', 'rows', 'rowLimit', 'estimatedBytes', 'byteLimit', 'utilizationPercent'];
  return value.map((raw, index) => {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error(`Warning[${index}] must be an object`);
    const warning = raw as Record<string, unknown>;
    if (Object.keys(warning).length !== keys.length || keys.some((key) => !(key in warning))) {
      throw new Error(`Warning[${index}] has unknown or missing fields`);
    }
    if (warning.code !== 'transactional_migration_utilization') throw new Error(`Warning[${index}] code is invalid`);
    for (const key of keys.slice(1)) {
      if (!Number.isSafeInteger(warning[key]) || (warning[key] as number) < 0) throw new Error(`Warning[${index}] ${key} is invalid`);
    }
    if ((warning.rows as number) > (warning.rowLimit as number) ||
        (warning.estimatedBytes as number) > (warning.byteLimit as number) ||
        warning.rowLimit !== MIGRATION_WARNING_ROW_LIMIT ||
        warning.byteLimit !== MIGRATION_WARNING_BYTE_LIMIT ||
        (warning.utilizationPercent as number) < 80 ||
        (warning.utilizationPercent as number) > 100) {
      throw new Error(`Warning[${index}] exceeds its bounds`);
    }
    return warning as MigrationWarning;
  });
}

export function validateRollbackResponse(value: unknown): DeploymentRollbackResponse {
  if (typeof value !== 'object' || value === null) throw new Error('Rollback response must be an object');
  const o = value as Record<string, unknown>;
  if (!isIdentifier(o.deploymentId)) throw new Error('deploymentId is invalid');
  if (!isIsoDateString(o.rolledBackAt)) throw new Error('rolledBackAt must be an ISO date string');
  const restoredDeploymentId = 'restoredDeploymentId' in o && o.restoredDeploymentId !== undefined ? o.restoredDeploymentId : undefined;
  if (restoredDeploymentId !== undefined && !isIdentifier(restoredDeploymentId)) throw new Error('restoredDeploymentId is invalid');
  return { deploymentId: o.deploymentId, rolledBackAt: o.rolledBackAt, restoredDeploymentId: restoredDeploymentId as string | undefined };
}

function validateDeploymentBundle(value: unknown): DeploymentBundle {
  if (typeof value !== 'object' || value === null) throw new Error('Bundle must be an object');
  const o = value as Record<string, unknown>;
  if (typeof o.js !== 'string' || !isBase64String(o.js)) throw new Error('bundle.js must be a base64 string');
  if (!isSha256Hex(o.sha256)) throw new Error('bundle.sha256 must be a hex SHA-256');
  if (!isNonNegativeInteger(o.size)) throw new Error('bundle.size must be a non-negative integer');
  return { js: o.js, sha256: o.sha256, size: o.size };
}

function validateFunctions(value: unknown): FunctionDescriptor[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value)) throw new Error('functions must be an array');
  return value.map((f, i) => validateFunctionDescriptor(f, i));
}

function validateFunctionDescriptor(value: unknown, index: number): FunctionDescriptor {
  if (typeof value !== 'object' || value === null) throw new Error(`Function[${index}] must be an object`);
  const o = value as Record<string, unknown>;
  if (!isIdentifier(o.name)) throw new Error(`Function[${index}] name is invalid`);
  if (!isFunctionType(o.type)) throw new Error(`Function[${index}] type is invalid`);
  if (!isFunctionVisibility(o.visibility)) throw new Error(`Function[${index}] visibility must be public or internal`);
  if (o.type === 'httpAction' && o.visibility !== 'public') {
    throw new Error(`Function[${index}] httpAction visibility must be public`);
  }
  if (!isModulePath(o.modulePath)) throw new Error(`Function[${index}] modulePath is invalid`);
  if (!isExportName(o.exportName)) throw new Error(`Function[${index}] exportName is invalid`);
  if ('args' in o && o.args !== undefined && !isValidValidatorDescriptor(o.args)) {
    throw new Error(`Function[${index}] args is not a valid validator descriptor`);
  }
  if ('returns' in o && o.returns !== undefined && !isValidValidatorDescriptor(o.returns)) {
    throw new Error(`Function[${index}] returns is not a valid validator descriptor`);
  }
  const route = validateRoute(o.route, index);
  if (route === undefined && o.type === 'httpAction') {
    throw new Error(`Function[${index}] httpAction requires route`);
  }
  if (route !== undefined && o.type !== 'httpAction') {
    throw new Error(`Function[${index}] route is only valid for httpAction`);
  }
  return {
    name: o.name,
    type: o.type,
    visibility: o.visibility,
    modulePath: o.modulePath,
    exportName: o.exportName,
    args: o.args as JSONValue | undefined,
    returns: o.returns as JSONValue | undefined,
    route,
  };
}

function validateRoute(value: unknown, index: number): { method: string; path?: string; pathPrefix?: string } | undefined {
  if (value === undefined) return undefined;
  if (typeof value !== 'object' || value === null) {
    throw new Error(`Function[${index}] route must be an object`);
  }
  const o = value as Record<string, unknown>;
  for (const key of Object.keys(o)) {
    if (key !== 'method' && key !== 'path' && key !== 'pathPrefix') {
      throw new Error(`Function[${index}] route has unknown fields`);
    }
  }
  if (typeof o.method !== 'string' || !isHttpMethod(o.method)) {
    throw new Error(`Function[${index}] route method is invalid`);
  }
  const pathPresent = Object.prototype.hasOwnProperty.call(o, 'path');
  const pathPrefixPresent = Object.prototype.hasOwnProperty.call(o, 'pathPrefix');
  if (pathPresent && typeof o.path !== 'string') {
    throw new Error(`Function[${index}] route path must be a string`);
  }
  if (pathPrefixPresent && typeof o.pathPrefix !== 'string') {
    throw new Error(`Function[${index}] route pathPrefix must be a string`);
  }
  const path = pathPresent ? o.path as string : undefined;
  const pathPrefix = pathPrefixPresent ? o.pathPrefix as string : undefined;
  if (pathPresent === pathPrefixPresent) {
    throw new Error(`Function[${index}] route must have exactly one of path or pathPrefix`);
  }
  if (path === '' || pathPrefix === '') {
    throw new Error(`Function[${index}] route path must not be empty`);
  }
  if (typeof path !== 'string' && typeof pathPrefix === 'string' && !pathPrefix.endsWith('/')) {
    throw new Error(`Function[${index}] route pathPrefix must end with /`);
  }
  if (pathPrefix !== undefined && pathPrefix.startsWith('/')) {
    throw new Error(`Function[${index}] route pathPrefix must be relative`);
  }
  if (path !== undefined && path.startsWith('/')) {
    throw new Error(`Function[${index}] route path must be relative`);
  }
  if (isReservedHttpActionPath(path ?? pathPrefix!)) {
    throw new Error(`Function[${index}] route uses a reserved platform path`);
  }
  return { method: o.method, path, pathPrefix };
}

function isReservedHttpActionPath(path: string): boolean {
  const segment = path.replace(/\/$/, '').split('/', 1)[0];
  return ['call', 'realtime', 'deployments', 'jobs', 'storage', 'admin'].includes(segment);
}

function validateConfig(value: unknown): Partial<DeploymentConfig> | undefined {
  if (value === undefined) return undefined;
  if (typeof value !== 'object' || value === null) throw new Error('config must be an object');
  const o = value as Record<string, unknown>;
  const config: Record<string, unknown> = {};
  if ('httpPathPrefix' in o && o.httpPathPrefix !== undefined) {
    if (typeof o.httpPathPrefix !== 'string' || o.httpPathPrefix !== DEFAULT_CONFIG.httpPathPrefix) {
      throw new Error(`config.httpPathPrefix is immutable; must be "${DEFAULT_CONFIG.httpPathPrefix}"`);
    }
    config.httpPathPrefix = o.httpPathPrefix;
  }
  if ('realtimePath' in o && o.realtimePath !== undefined) {
    if (typeof o.realtimePath !== 'string' || o.realtimePath !== DEFAULT_CONFIG.realtimePath) {
      throw new Error(`config.realtimePath is immutable; must be "${DEFAULT_CONFIG.realtimePath}"`);
    }
    config.realtimePath = o.realtimePath;
  }
  if ('maxUploadBytes' in o && o.maxUploadBytes !== undefined) {
    if (!isNonNegativeInteger(o.maxUploadBytes) || o.maxUploadBytes > MAX_DEPLOYMENT_UPLOAD_BYTES) throw new Error('config.maxUploadBytes must be a non-negative integer');
    config.maxUploadBytes = o.maxUploadBytes;
  }
  if ('maxFunctionArgsBytes' in o && o.maxFunctionArgsBytes !== undefined) {
    if (!isNonNegativeInteger(o.maxFunctionArgsBytes) || o.maxFunctionArgsBytes > MAX_FUNCTION_ARGS_LIMIT) throw new Error('config.maxFunctionArgsBytes must be a non-negative integer <= ' + MAX_FUNCTION_ARGS_LIMIT);
    config.maxFunctionArgsBytes = o.maxFunctionArgsBytes;
  }
  if ('maxReturnValueBytes' in o && o.maxReturnValueBytes !== undefined) {
    if (!isNonNegativeInteger(o.maxReturnValueBytes) || o.maxReturnValueBytes > MAX_RETURN_VALUE_LIMIT) throw new Error('config.maxReturnValueBytes must be a non-negative integer <= ' + MAX_RETURN_VALUE_LIMIT);
    config.maxReturnValueBytes = o.maxReturnValueBytes;
  }
  if ('defaultRequestTimeoutMs' in o && o.defaultRequestTimeoutMs !== undefined) {
    if (!isNonNegativeInteger(o.defaultRequestTimeoutMs)) throw new Error('config.defaultRequestTimeoutMs must be a non-negative integer');
    config.defaultRequestTimeoutMs = o.defaultRequestTimeoutMs;
  }
  return Object.keys(config).length > 0 ? (config as Partial<DeploymentConfig>) : undefined;
}

function validateSchema(value: unknown): SchemaDescriptor {
  if (typeof value !== 'object' || value === null) throw new Error('schema must be an object');
  const o = value as Record<string, unknown>;
  if (!Array.isArray(o.tables)) throw new Error('schema.tables must be an array');
  return { tables: o.tables.map((t, i) => validateTableDescriptor(t, i)) };
}

function validateTableDescriptor(value: unknown, index: number): TableDescriptor {
  if (typeof value !== 'object' || value === null) throw new Error(`schema.tables[${index}] must be an object`);
  const o = value as Record<string, unknown>;
  if (!isIdentifier(o.tableName) || o.tableName.toLowerCase().startsWith('pbvex_cmp_')) {
    throw new Error(`schema.tables[${index}] tableName is invalid`);
  }
  if (typeof o.fields !== 'object' || o.fields === null || Array.isArray(o.fields)) {
    throw new Error(`schema.tables[${index}] fields must be an object`);
  }
  const fields: Record<string, JSONValue> = {};
  for (const [k, v] of Object.entries(o.fields)) {
    if (!isSafeFieldName(k)) throw new Error(`schema.tables[${index}] invalid field name ${k}`);
    if (!isValidValidatorDescriptor(v)) {
      throw new Error(`schema.tables[${index}] field ${k} is not a valid validator descriptor`);
    }
    fields[k] = v as JSONValue;
  }
  let indexes: IndexDescriptor[] | undefined;
  if ('indexes' in o && o.indexes !== undefined) {
    if (!Array.isArray(o.indexes)) throw new Error(`schema.tables[${index}] indexes must be an array`);
    indexes = o.indexes.map((idx, i) => validateIndexDescriptor(idx, i, index));
  }
  return { tableName: o.tableName, fields, indexes };
}

const DESCRIPTOR_ALLOWED_KEYS: Record<string, readonly string[]> = {
  string: ['type'], number: ['type'], int64: ['type'], float64: ['type'],
  bytes: ['type'], boolean: ['type'], any: ['type'], null: ['type'],
  id: ['type', 'tableName'], literal: ['type', 'value'], array: ['type', 'item'],
  record: ['type', 'key', 'value'], object: ['type', 'shape', 'fields'], union: ['type', 'validators'],
  optional: ['type', 'validator'], defaulted: ['type', 'validator', 'defaultValue'],
  recursive: ['type', 'name', 'validator'], ref: ['type', 'name'],
};

function descriptorOnlyKeys(o: Record<string, unknown>, allowed: readonly string[]): boolean {
  for (const k of Object.keys(o)) {
    if (!allowed.includes(k)) return false;
  }
  return true;
}

function descriptorShape(o: Record<string, unknown>): Record<string, unknown> | undefined {
  const shape = o.shape;
  if (shape !== undefined && shape !== null && typeof shape === 'object' && !Array.isArray(shape)) {
    return shape as Record<string, unknown>;
  }
  const fields = o.fields;
  if (fields !== undefined && fields !== null && typeof fields === 'object' && !Array.isArray(fields)) {
    return fields as Record<string, unknown>;
  }
  return undefined;
}

/**
 * Validates a PBVex validator descriptor. Mirrors the Go schema validator
 * (schema.ValidateDescriptor) so the TS and backend layers enforce one
 * deployable, executable descriptor contract: the same types, keys, and
 * wire-encoded default/literal values. Recursive types use
 * `{type:'recursive', name, validator}` with `{type:'ref', name}` cycle points;
 * refs must resolve to a name declared by an enclosing recursive. `delayed` is
 * never deployable (its closure has no executable descriptor).
 */
export function isValidValidatorDescriptor(value: unknown): boolean {
  return isValidValidatorDescriptorWith(value, new Map<string, unknown>(), 0, { nodes: 0, bytes: 0 });
}

interface Budget { nodes: number; bytes: number; }

function isValidValidatorDescriptorWith(value: unknown, definitions: Map<string, unknown>, depth: number, budget: Budget): boolean {
  if (depth > MAX_VALUE_DEPTH || budget.nodes >= MAX_BUDGET_NODES) return false;
  budget.nodes++;
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return false;
  const o = value as Record<string, unknown>;
  const type = o.type;
  if (typeof type !== 'string') return false;
  const allowed = DESCRIPTOR_ALLOWED_KEYS[type];
  if (!allowed) return false;
  switch (type) {
    case 'string':
    case 'number':
    case 'int64':
    case 'float64':
    case 'bytes':
    case 'boolean':
    case 'any':
    case 'null':
      return descriptorOnlyKeys(o, allowed);
    case 'id':
      return typeof o.tableName === 'string' && isIdentifier(o.tableName) && descriptorOnlyKeys(o, allowed);
    case 'literal':
      return 'value' in o && isLiteralWireValue(o.value) && descriptorOnlyKeys(o, allowed);
    case 'array':
      return descriptorOnlyKeys(o, allowed) && isValidValidatorDescriptorWith(o.item, definitions, depth + 1, budget);
    case 'record':
      return (
        descriptorOnlyKeys(o, allowed) &&
        isValidRecordKeyDescriptor(o.key) &&
        isValidValidatorDescriptorWith(o.key, definitions, depth + 1, budget) &&
        isValidValidatorDescriptorWith(o.value, definitions, depth + 1, budget)
      );
    case 'object': {
      if (!descriptorOnlyKeys(o, allowed)) return false;
      const keys = Object.keys(o);
      if (keys.length === 1) return true;
      if (keys.length !== 2) return false;
      const shape = descriptorShape(o);
      if (!shape) return false;
      if (Object.keys(shape).length > MAX_MAP_SIZE) return false;
      for (const [k, v] of Object.entries(shape)) {
        if (!isSafeFieldName(k) || !isValidValidatorDescriptorWith(v, definitions, depth + 1, budget)) return false;
      }
      return true;
    }
    case 'union': {
      if (!descriptorOnlyKeys(o, allowed)) return false;
      if (!Array.isArray(o.validators) || o.validators.length === 0 || o.validators.length > MAX_UNION_BRANCHES) return false;
      return o.validators.every((v) => isValidValidatorDescriptorWith(v, definitions, depth + 1, budget));
    }
    case 'optional':
      return descriptorOnlyKeys(o, allowed) && isValidValidatorDescriptorWith(o.validator, definitions, depth + 1, budget);
    case 'defaulted':
      return (
        'defaultValue' in o &&
        descriptorOnlyKeys(o, allowed) &&
        isValidValidatorDescriptorWith(o.validator, definitions, depth + 1, budget) &&
        wireValueValidates(o.validator, o.defaultValue, definitions, depth + 1, budget, new Set<object>())
      );
    case 'recursive': {
      if (typeof o.name !== 'string' || !isIdentifier(o.name) || !descriptorOnlyKeys(o, allowed)) {
        return false;
      }
      const next = new Map(definitions);
      next.set(o.name, o.validator);
      return isValidValidatorDescriptorWith(o.validator, next, depth + 1, budget);
    }
    case 'ref':
      return typeof o.name === 'string' && isIdentifier(o.name) && definitions.has(o.name) && descriptorOnlyKeys(o, allowed);
    default:
      return false;
  }
}

function isValidRecordKeyDescriptor(value: unknown): boolean {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return false;
  const o = value as Record<string, unknown>;
  const keys = Object.keys(o);
  switch (o.type) {
    case 'string':
      return keys.length === 1;
    case 'literal':
      return keys.length === 2 && typeof o.value === 'string' && isSafeFieldName(o.value);
    case 'union':
      if (keys.length !== 2 || !Array.isArray(o.validators) || o.validators.length === 0 || o.validators.length > MAX_UNION_BRANCHES) return false;
      return o.validators.every(isValidRecordKeyDescriptor);
    default:
      return false;
  }
}

function wireValueValidates(
  descriptor: unknown,
  value: unknown,
  definitions: Map<string, unknown>,
  depth: number,
  budget: Budget,
  seen: Set<object>,
): boolean {
  if (descriptor === null || typeof descriptor !== 'object') return false;
  budget.nodes++;
  budget.bytes += valueSize(value);
  if (budget.nodes > MAX_BUDGET_NODES || budget.bytes > MAX_BUDGET_BYTES) return false;
  const o = descriptor as Record<string, unknown>;
  switch (o.type) {
    case 'string':
      return typeof value === 'string';
    case 'number':
    case 'float64':
      return typeof value === 'number' && Number.isFinite(value);
    case 'int64':
      return isWireInteger(value);
    case 'bytes':
      return isWireBytes(value);
    case 'boolean':
      return typeof value === 'boolean';
    case 'null':
      return value === null;
    case 'any':
      return validateWireValue(value, depth + 1, seen, budget, false);
    case 'literal':
      return isLiteralWireValue(o.value) && wireEqual(o.value, value);
    case 'id': {
      if (typeof value !== 'string') return false;
      const table = typeof o.tableName === 'string' ? o.tableName : '';
      const [target, ok] = opaqueIDTarget(value);
      return ok && target === table;
    }
    case 'object': {
      const shape = descriptorShape(o);
      if (!shape) return validateWireValue(value, depth + 1, seen, budget, false);
      if (value === null || typeof value !== 'object' || Array.isArray(value)) return false;
      if (seen.has(value as object)) return false;
      seen.add(value as object);
      try {
        const valueMap = value as Record<string, unknown>;
        if (Object.keys(valueMap).length > MAX_MAP_SIZE) return false;
        for (const k of Object.keys(valueMap)) {
          if (!(k in shape)) return false;
        }
        for (const [k, v] of Object.entries(shape)) {
          if (!wireValueValidates(v, valueMap[k], definitions, depth + 1, budget, seen)) return false;
        }
        return true;
      } finally {
        seen.delete(value as object);
      }
    }
    case 'array': {
      if (!Array.isArray(value)) return false;
      if (value.length > MAX_ARRAY_SIZE) return false;
      if (seen.has(value as object)) return false;
      seen.add(value as object);
      try {
        return value.every((item) => wireValueValidates(o.item, item, definitions, depth + 1, budget, seen));
      } finally {
        seen.delete(value as object);
      }
    }
    case 'record': {
      if (value === null || typeof value !== 'object' || Array.isArray(value)) return false;
      if (seen.has(value as object)) return false;
      seen.add(value as object);
      try {
        const valueMap = value as Record<string, unknown>;
        const keys = Object.keys(valueMap);
        if (keys.length > MAX_MAP_SIZE) return false;
        return keys.every((k) =>
          isSafeFieldName(k) && wireValueValidates(o.key, k, definitions, depth + 1, budget, seen) && wireValueValidates(o.value, valueMap[k], definitions, depth + 1, budget, seen),
        );
      } finally {
        seen.delete(value as object);
      }
    }
    case 'union':
      return Array.isArray(o.validators) && o.validators.some((b) => wireValueValidates(b, value, definitions, depth + 1, budget, seen));
    case 'optional':
      return value === undefined || wireValueValidates(o.validator, value, definitions, depth + 1, budget, seen);
    case 'defaulted':
      if (value === undefined) {
        return wireValueValidates(o.validator, o.defaultValue, definitions, depth + 1, budget, seen);
      }
      return wireValueValidates(o.validator, value, definitions, depth + 1, budget, seen);
    case 'recursive': {
      const next = new Map(definitions);
      next.set(o.name as string, o.validator);
      return wireValueValidates(o.validator, value, next, depth + 1, budget, seen);
    }
    case 'ref': {
      const target = definitions.get(o.name as string);
      return target !== undefined && wireValueValidates(target, value, definitions, depth + 1, budget, seen);
    }
    default:
      return false;
  }
}

function wireEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (a === null || b === null) return a === b;
  if (typeof a !== 'object' || typeof b !== 'object') return a === b;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  if (Array.isArray(a)) {
    const bArr = b as unknown[];
    return a.length === bArr.length && a.every((v, i) => wireEqual(v, bArr[i]));
  }
  const aMap = a as Record<string, unknown>;
  const bMap = b as Record<string, unknown>;
  const aKeys = Object.keys(aMap);
  const bKeys = Object.keys(bMap);
  if (aKeys.length !== bKeys.length) return false;
  return aKeys.every((k) => k in bMap && wireEqual(aMap[k], bMap[k]));
}

function opaqueIDTarget(value: string): [string, boolean] {
  const shared = parseOpaqueId(value);
  return shared ? [shared.table, true] : ['', false];
}

function validateWireValue(value: unknown, depth: number, seen: Set<object>, budget: Budget, account: boolean): boolean {
  if (depth > MAX_VALUE_DEPTH) return false;
  if (account) {
    budget.nodes++;
    budget.bytes += valueSize(value);
    if (budget.nodes > MAX_BUDGET_NODES || budget.bytes > MAX_BUDGET_BYTES) return false;
  }
  if (value === null || typeof value === 'boolean' || typeof value === 'string') return true;
  if (typeof value === 'number') return Number.isFinite(value);
  if (typeof value !== 'object') return false;
  if (seen.has(value as object)) return false;
  seen.add(value as object);
  try {
    if (Array.isArray(value)) {
      if (value.length > MAX_ARRAY_SIZE) return false;
      return value.every((item) => validateWireValue(item, depth + 1, seen, budget, true));
    }
    const map = value as Record<string, unknown>;
    const keys = Object.keys(map);
    if (keys.length > MAX_MAP_SIZE) return false;
    if (keys.length === 1) {
      const marker = wireMarker(value);
      if (marker === '$integer') return isWireInteger(value);
      if (marker === '$bytes') return isWireBytes(value);
      if (marker !== null) return false;
    }
    return keys.every((k) => isSafeFieldName(k) && validateWireValue(map[k], depth + 1, seen, budget, true));
  } finally {
    seen.delete(value as object);
  }
}

function utf8ByteLength(s: string): number {
  let len = 0;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < 0x80) len++;
    else if (c < 0x800) len += 2;
    else if (c >= 0xd800 && c <= 0xdbff) { len += 4; i++; }
    else len += 3;
  }
  return len;
}

function valueSize(value: unknown): number {
  if (typeof value === 'string') return utf8ByteLength(value);
  if (Array.isArray(value)) return value.length + 1;
  if (value !== null && typeof value === 'object') {
    const map = value as Record<string, unknown>;
    let n = 1;
    for (const [key, item] of Object.entries(map)) {
      n += utf8ByteLength(key);
      if (typeof item === 'string') n += utf8ByteLength(item);
      else if (Array.isArray(item)) n += item.length;
      else if (item !== null && typeof item === 'object') n += Object.keys(item).length;
      else n++;
    }
    return n;
  }
  return 1;
}

/** Mirrors Go isLiteralValue: any JSON value, or a single-key $integer wire object. */
function isLiteralWireValue(value: unknown): boolean {
  const marker = wireMarker(value);
  if (marker === '$integer') return isWireInteger(value);
  if (marker !== null) return false;
  return isJsonValue(value);
}

/** Mirrors Go isEncodedValue: any JSON value, or a single-key $integer/$bytes wire object. */
function isEncodedWireValue(value: unknown): boolean {
  const marker = wireMarker(value);
  if (marker === '$integer') return isWireInteger(value);
  if (marker === '$bytes') return isWireBytes(value);
  if (marker !== null) return false;
  return isJsonValue(value);
}

function wireMarker(value: unknown): string | null {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return null;
  const keys = Object.keys(value);
  if (keys.length !== 1 || !keys[0]!.startsWith('$')) return null;
  return keys[0]!;
}

function isWireInteger(value: unknown): boolean {
  const raw = wirePayload(value, '$integer');
  return raw !== undefined && base64DecodedLength(raw) === 8;
}

function isWireBytes(value: unknown): boolean {
  return wirePayload(value, '$bytes') !== undefined;
}

function wirePayload(value: unknown, key: string): string | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined;
  const entries = Object.entries(value);
  if (entries.length !== 1 || entries[0]![0] !== key) return undefined;
  const v = entries[0]![1];
  return typeof v === 'string' && isBase64String(v) ? v : undefined;
}

function base64DecodedLength(value: string): number {
  try {
    return atob(value).length;
  } catch {
    return -1;
  }
}

function validateIndexDescriptor(value: unknown, index: number, tableIndex: number): IndexDescriptor {
  if (typeof value !== 'object' || value === null) {
    throw new Error(`schema.tables[${tableIndex}].indexes[${index}] must be an object`);
  }
  const o = value as Record<string, unknown>;
  if (!isIdentifier(o.name)) throw new Error(`schema.tables[${tableIndex}].indexes[${index}] name is invalid`);
  if (!Array.isArray(o.fields) || !o.fields.every((f) => typeof f === 'string')) {
    throw new Error(`schema.tables[${tableIndex}].indexes[${index}] fields must be an array of strings`);
  }
  return { name: o.name, fields: o.fields as string[] };
}

function base64ToArrayBuffer(value: string): ArrayBuffer {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer;
}
