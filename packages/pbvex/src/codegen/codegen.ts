import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import type { FunctionDefinition } from '../runtime/server.js';
import { ValidationError } from '../runtime/values.js';
import type { SchemaDefinition, TableDefinition } from '../schema/schema.js';
import { deriveFunctionName } from '../bundler/functionName.js';
import { validatorToTypeString, collectRecursive, emitRecursiveAliases, isEmptyArgsDescriptor, RecursiveRegistry } from './types.js';

export interface CodegenOptions {
  rootDir: string;
  functions: FunctionDefinition<any, any, any>[];
  project?: string;
  emailTemplateNames?: string[];
}

const RESERVED_TS_IDENTIFIERS = new Set([
  'break',
  'case',
  'catch',
  'class',
  'const',
  'continue',
  'debugger',
  'default',
  'delete',
  'do',
  'else',
  'enum',
  'export',
  'extends',
  'false',
  'finally',
  'for',
  'function',
  'if',
  'import',
  'in',
  'instanceof',
  'new',
  'null',
  'return',
  'super',
  'switch',
  'this',
  'throw',
  'true',
  'try',
  'typeof',
  'var',
  'void',
  'while',
  'with',
  'let',
  'interface',
  'type',
  'declare',
  'module',
  'namespace',
  'abstract',
  'as',
  'async',
  'await',
  'constructor',
  'from',
  'get',
  'is',
  'of',
  'set',
  'yield',
  'implements',
  'private',
  'protected',
  'public',
  'readonly',
  'static',
  'symbol',
  'unique',
  'never',
  'any',
  'unknown',
  'object',
  'bigint',
  'boolean',
  'number',
  'string',
  'undefined',
]);

function tsIdentifier(name: string): string {
  return RESERVED_TS_IDENTIFIERS.has(name) ? `${name}_` : name;
}

function splitModulePath(modulePath: string): string[] {
  return modulePath.replace(/^pbvex\//, '').replace(/\.ts$/, '').split('/');
}

interface ApiNode {
  [key: string]: ApiNode | FunctionDefinition<any, any, any>;
}

function isFunctionDefinition(value: unknown): boolean {
  if (value === null || typeof value !== 'object') return false;
  const obj = value as Record<string, unknown>;
  return ['query', 'mutation', 'action', 'httpAction'].includes(obj.type as string) && typeof obj.handler === 'function';
}

function buildApiTree(
  functions: FunctionDefinition<any, any, any>[],
  visibility: 'public' | 'internal',
): ApiNode {
  const tree: ApiNode = {};
  for (const fn of functions) {
    if (fn.visibility !== visibility) continue;
    const parts = splitModulePath(fn.modulePath);
    let node: ApiNode = tree;
    let pathSoFar = '';
    for (const part of parts) {
      pathSoFar = pathSoFar ? `${pathSoFar}.${part}` : part;
      const existing = node[part];
      if (existing !== undefined) {
        if (isFunctionDefinition(existing)) {
          const existingFn = existing as FunctionDefinition<any, any, any>;
          throw new ValidationError(
            `API tree conflict: module ${JSON.stringify(fn.modulePath)} export ${JSON.stringify(fn.exportName)} ` +
              `collides with function ${JSON.stringify(existingFn.modulePath)}#${JSON.stringify(existingFn.exportName)} at ${JSON.stringify(pathSoFar)}`,
          );
        }
      } else {
        node[part] = {};
      }
      node = node[part] as ApiNode;
    }
    const existingExport = node[fn.exportName];
    if (existingExport !== undefined) {
      if (isFunctionDefinition(existingExport)) {
        const existingFn = existingExport as FunctionDefinition<any, any, any>;
        throw new ValidationError(
          `API tree conflict: module ${JSON.stringify(fn.modulePath)} export ${JSON.stringify(fn.exportName)} ` +
            `collides with function ${JSON.stringify(existingFn.modulePath)}#${JSON.stringify(existingFn.exportName)} at ${JSON.stringify(pathSoFar ? `${pathSoFar}.${fn.exportName}` : fn.exportName)}`,
        );
      }
      throw new ValidationError(
        `API tree conflict: module ${JSON.stringify(fn.modulePath)} export ${JSON.stringify(fn.exportName)} ` +
          `collides with an existing namespace at ${JSON.stringify(pathSoFar ? `${pathSoFar}.${fn.exportName}` : fn.exportName)}`,
      );
    }
    node[fn.exportName] = fn;
  }
  return tree;
}

function renderApiTree(
  node: ApiNode,
  indent: string,
  registry: RecursiveRegistry,
  argsJson: Map<FunctionDefinition<any, any, any>, unknown>,
  returnsJson: Map<FunctionDefinition<any, any, any>, unknown>,
): string {
  const lines: string[] = [];
  const keys = Object.keys(node).sort();
  for (const key of keys) {
    const value = node[key];
    if (isFunctionDefinition(value)) {
      const fn = value as FunctionDefinition<any, any, any>;
      const argsDescriptor = argsJson.get(fn);
      const argsType = validatorToTypeString(argsDescriptor, registry, []);
      const returnsType = validatorToTypeString(returnsJson.get(fn), registry, []);
      const functionName = deriveFunctionName(fn.modulePath, fn.exportName);
      const noArgs = isEmptyArgsDescriptor(argsDescriptor);
      const body = noArgs
        ? `{ "_path": ${JSON.stringify(functionName)}, "_type": ${JSON.stringify(fn.type)}, "_visibility": ${JSON.stringify(fn.visibility)}, "__noArgs": true }`
        : `{ "_path": ${JSON.stringify(functionName)}, "_type": ${JSON.stringify(fn.type)}, "_visibility": ${JSON.stringify(fn.visibility)} }`;
      lines.push(
        `${indent}${JSON.stringify(key)}: ${body} as FunctionReference<${JSON.stringify(fn.type)}, ${argsType}, ${returnsType}, ${JSON.stringify(fn.visibility)}>,`
      );
    } else {
      lines.push(`${indent}${JSON.stringify(key)}: {`);
      lines.push(renderApiTree(value as ApiNode, indent + '  ', registry, argsJson, returnsJson));
      lines.push(`${indent}},`);
    }
  }
  return lines.join('\n');
}

export function generateApiTs(functions: FunctionDefinition<any, any, any>[]): string {
  const publicTree = buildApiTree(functions, 'public');
  const internalTree = buildApiTree(functions, 'internal');
  const taken = new Set<string>(['api', 'internal', 'ApiDefinition', 'InternalApiDefinition', 'FunctionReference', 'Id']);
  const registry = new RecursiveRegistry(taken);

  const argsJson = new Map<FunctionDefinition<any, any, any>, unknown>();
  const returnsJson = new Map<FunctionDefinition<any, any, any>, unknown>();
  const sortedFunctions = [...functions].sort((a, b) => {
    const aKey = `${a.modulePath}:${a.exportName}`;
    const bKey = `${b.modulePath}:${b.exportName}`;
    return aKey < bKey ? -1 : aKey > bKey ? 1 : 0;
  });
  for (const fn of sortedFunctions) {
    const aj = (fn.args as any).toJSON();
    const rj = (fn.returns as any).toJSON();
    argsJson.set(fn, aj);
    returnsJson.set(fn, rj);
    collectRecursive(aj, registry, []);
    collectRecursive(rj, registry, []);
  }

  const publicBody = Object.keys(publicTree).length > 0 ? renderApiTree(publicTree, '  ', registry, argsJson, returnsJson) : '  // no public functions';
  const internalBody = Object.keys(internalTree).length > 0 ? renderApiTree(internalTree, '  ', registry, argsJson, returnsJson) : '  // no internal functions';
  const aliases = emitRecursiveAliases(registry);

  return `// Generated by PBVex - do not edit manually
import type { FunctionReference } from 'pbvex/server';
import type { Id } from './dataModel.js';
${aliases.length > 0 ? `\n${aliases.join('\n')}\n` : ''}
export const api = {
${publicBody}
} as const;

export const internal = {
${internalBody}
} as const;

export type ApiDefinition = typeof api;
export type InternalApiDefinition = typeof internal;
`;
}

function getFieldJson(validator: unknown): Record<string, unknown> | undefined {
  if (validator === null || typeof validator !== 'object') return undefined;
  const v = validator as Record<string, unknown>;
  if (typeof v.toJSON !== 'function') return undefined;
  return v.toJSON() as Record<string, unknown>;
}

function isDocOptional(validator: unknown): boolean {
  const json = getFieldJson(validator);
  return json?.type === 'optional';
}

function isInsertOptional(validator: unknown): boolean {
  const json = getFieldJson(validator);
  return json?.type === 'optional' || json?.type === 'defaulted';
}

export function generateDataModelTs(schema: SchemaDefinition | undefined): string {
  const lines: string[] = [];
  lines.push(`// Generated by PBVex - do not edit manually`);
  lines.push(`import type { GenericId, GenericDocument, TableInfo } from 'pbvex/server';`);
  lines.push('');

  const tables = schema ? Object.keys(schema.tables).sort() : [];

  const taken = new Set<string>(['TableNames', 'Doc', 'Id', 'DataModel', 'GenericDocument', 'GenericId', 'TableInfo']);
  for (const tableName of tables) {
    const safeName = tsIdentifier(tableName);
    taken.add(safeName);
    taken.add(`${safeName}Insert`);
  }
  const registry = new RecursiveRegistry(taken);

  const fieldJson = new Map<string, unknown>();
  for (const tableName of tables) {
    const table = schema!.tables[tableName]!;
    for (const field of Object.keys(table.fields).sort()) {
      const json = getFieldJson(table.fields[field]);
      fieldJson.set(`${tableName}.${field}`, json);
      collectRecursive(json, registry, []);
    }
  }
  const aliases = emitRecursiveAliases(registry);
  if (aliases.length > 0) {
    lines.push(aliases.join('\n'));
    lines.push('');
  }

  if (tables.length > 0) {
    lines.push(`export type TableNames = ${tables.map((t) => `'${t}'`).join(' | ')};`);
    lines.push('');
    lines.push(`export type Doc<TableName extends TableNames> = DataModel[TableName]['document'];`);
    lines.push(`export type Id<TableName extends TableNames> = GenericId<TableName>;`);
    lines.push('');
  } else {
    lines.push(`export type TableNames = never;`);
    lines.push(`export type Doc<TableName extends TableNames> = never;`);
    lines.push(`export type Id<TableName extends TableNames> = GenericId<TableName>;`);
    lines.push('');
  }

  for (const tableName of tables) {
    const table = schema!.tables[tableName]!;
    const safeName = tsIdentifier(tableName);
    const fieldKeys = Object.keys(table.fields).sort();

    lines.push(`export interface ${safeName} extends GenericDocument {`);
    lines.push(`  ${JSON.stringify('_id')}: Id<${JSON.stringify(tableName)}>;`);
    lines.push(`  ${JSON.stringify('_creationTime')}: number;`);
    for (const field of fieldKeys) {
      const validator = table.fields[field];
      const typeString = validatorToTypeString(fieldJson.get(`${tableName}.${field}`), registry, []);
      const optional = isDocOptional(validator) ? '?' : '';
      lines.push(`  ${JSON.stringify(field)}${optional}: ${typeString};`);
    }
    lines.push(`}`);
    lines.push('');

    lines.push(`export interface ${safeName}Insert {`);
    for (const field of fieldKeys) {
      const validator = table.fields[field];
      const typeString = validatorToTypeString(fieldJson.get(`${tableName}.${field}`), registry, []);
      const optional = isInsertOptional(validator) ? '?' : '';
      lines.push(`  ${JSON.stringify(field)}${optional}: ${typeString};`);
    }
    lines.push(`}`);
    lines.push('');
  }

  if (tables.length > 0) {
    lines.push(`export type DataModel = {`);
    for (const tableName of tables) {
      const table = schema!.tables[tableName]!;
      const safeName = tsIdentifier(tableName);
      const indexEntries: string[] = [];
      if (table.indexes && table.indexes.length > 0) {
        for (const idx of table.indexes) {
          const fields = idx.fields.map((f) => JSON.stringify(f)).join(', ');
          indexEntries.push(`    ${JSON.stringify(idx.name)}: { fields: readonly [${fields}] }`);
        }
      }
      if (indexEntries.length > 0) {
        const indexesType = `{\n${indexEntries.join('\n')}\n  }`;
        lines.push(`  ${JSON.stringify(tableName)}: TableInfo<${safeName}, ${indexesType}, ${safeName}Insert>;`);
      } else {
        lines.push(`  ${JSON.stringify(tableName)}: TableInfo<${safeName}, never, ${safeName}Insert>;`);
      }
    }
    lines.push(`};`);
  } else {
    lines.push(`export type DataModel = {};`);
  }
  lines.push('');

  return lines.join('\n');
}

export function generateServerTs(emailTemplateNames: string[] = []): string {
  const emailTemplateName = emailTemplateNames.length ? emailTemplateNames.sort().map((name) => JSON.stringify(name)).join(' | ') : 'never';
  return `// Generated by PBVex - do not edit manually
import type { DataModel, Doc, Id, TableNames } from './dataModel.js';
import type { FunctionOptions, QueryDef, MutationDef, ActionDef, HttpActionDef } from 'pbvex/server';
import type { QueryCtx as GenericQueryCtx, MutationCtx as GenericMutationCtx, ActionCtx as GenericActionCtx, HttpActionCtx as GenericHttpActionCtx } from 'pbvex/server';
import type { Request, Response } from 'pbvex/server';
import { query as queryGeneric, mutation as mutationGeneric, action as actionGeneric, httpAction as httpActionGeneric } from 'pbvex/server';
import { internalQuery as internalQueryGeneric, internalMutation as internalMutationGeneric, internalAction as internalActionGeneric } from 'pbvex/server';

export type QueryCtx = GenericQueryCtx<DataModel>;
export type MutationCtx = GenericMutationCtx<DataModel>;
export type EmailTemplateName = ${emailTemplateName};
export type ActionCtx = GenericActionCtx<DataModel, EmailTemplateName>;
export type HttpActionCtx = GenericHttpActionCtx<DataModel, EmailTemplateName>;

export type { FunctionContext, AuthContext, UserIdentity, DatabaseReader, DatabaseWriter, SchedulerContext, SchedulableFunctionReference, JobId, JobID, StorageReader, StorageContext, StorageId, EmailContext, EmailSendOptions, EmailTemplateVariables, HttpContext, HttpSendOptions, HttpSendResponse, Request, Response, FunctionRoute } from 'pbvex/server';
export type { GenericDataModel, GenericId, GenericDocument, TableInfo, IndexInfo, TableNamesInDataModel, NamedTableInfo, DocumentByName, InsertByName, WithoutSystemFields, PatchValue, PaginationOptions, PaginationResult, QueryInitializer, Query, OrderedQuery, FilterBuilder, Expression, ExpressionOrValue, IndexRangeBuilder, IndexRange, Value, NumericValue } from 'pbvex/server';
export type { Doc, Id, TableNames } from './dataModel.js';

export const query = queryGeneric as <Args = Record<string, never>, Returns = any>(options: FunctionOptions<Args, Returns, QueryCtx>) => QueryDef<Args, Returns, QueryCtx>;
export const internalQuery = internalQueryGeneric as <Args = Record<string, never>, Returns = any>(options: FunctionOptions<Args, Returns, QueryCtx>) => QueryDef<Args, Returns, QueryCtx>;
export const mutation = mutationGeneric as <Args = Record<string, never>, Returns = any>(options: FunctionOptions<Args, Returns, MutationCtx>) => MutationDef<Args, Returns, MutationCtx>;
export const internalMutation = internalMutationGeneric as <Args = Record<string, never>, Returns = any>(options: FunctionOptions<Args, Returns, MutationCtx>) => MutationDef<Args, Returns, MutationCtx>;
export const action = actionGeneric as <Args = Record<string, never>, Returns = any>(options: FunctionOptions<Args, Returns, ActionCtx>) => ActionDef<Args, Returns, ActionCtx>;
export const internalAction = internalActionGeneric as <Args = Record<string, never>, Returns = any>(options: FunctionOptions<Args, Returns, ActionCtx>) => ActionDef<Args, Returns, ActionCtx>;
export const httpAction = httpActionGeneric as <Args extends Request = Request, Returns extends Response = Response>(options: FunctionOptions<Args, Returns, HttpActionCtx>) => HttpActionDef<Args, Returns, HttpActionCtx>;
`;
}

export async function generateCodegenFiles(options: CodegenOptions, schema?: SchemaDefinition): Promise<void> {
  const generatedDir = path.join(options.rootDir, 'pbvex', '_generated');
  await mkdir(generatedDir, { recursive: true });

  await Promise.all([
    writeFile(path.join(generatedDir, 'api.ts'), generateApiTs(options.functions), 'utf-8'),
    writeFile(path.join(generatedDir, 'dataModel.ts'), generateDataModelTs(schema), 'utf-8'),
    writeFile(path.join(generatedDir, 'server.ts'), generateServerTs(options.emailTemplateNames), 'utf-8'),
  ]);
}
