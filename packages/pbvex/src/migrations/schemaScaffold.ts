import { existsSync } from 'node:fs';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import type { JSONValue, TableDescriptor } from '@pbvex/protocol';
import { migrationSlug } from './pocketbaseScaffold.js';

export interface SchemaMigrationScaffoldOptions {
  rootDir: string;
  name: string;
  table: string;
  sourceTable?: TableDescriptor;
  targetTable: TableDescriptor;
  now?: Date;
}

export interface SchemaMigrationScaffoldResult {
  migrationPath: string;
  usedEmptySource: boolean;
}

function literalSource(value: JSONValue): string {
  if (value && typeof value === 'object' && !Array.isArray(value) && typeof value.$integer === 'string') {
    const bytes = Buffer.from(value.$integer, 'base64');
    if (bytes.length === 8) return `${bytes.readBigInt64LE()}n`;
  }
  return JSON.stringify(value);
}

export function validatorSource(raw: JSONValue): string {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('Invalid validator descriptor');
  const descriptor = raw as Record<string, JSONValue>;
  switch (descriptor.type) {
    case 'string': case 'number': case 'int64': case 'float64': case 'boolean': case 'bytes': case 'any':
      return `v.${descriptor.type}()`;
    case 'null': return 'v.null()';
    case 'id': return `v.id(${JSON.stringify(descriptor.tableName)})`;
    case 'literal': return `v.literal(${literalSource(descriptor.value!)})`;
    case 'array': return `v.array(${validatorSource(descriptor.item!)})`;
    case 'record': return `v.record(${validatorSource(descriptor.key!)}, ${validatorSource(descriptor.value!)})`;
    case 'union': return `v.union(${(descriptor.validators as JSONValue[]).map(validatorSource).join(', ')})`;
    case 'optional': return `v.optional(${validatorSource(descriptor.validator!)})`;
    case 'defaulted': return `v.defaulted(${validatorSource(descriptor.validator!)}, ${literalSource(descriptor.defaultValue!)})`;
    case 'object': {
      const shape = (descriptor.shape ?? descriptor.fields ?? {}) as Record<string, JSONValue>;
      const fields = Object.entries(shape).map(([name, validator]) =>
        `    ${JSON.stringify(name)}: ${validatorSource(validator)},`).join('\n');
      return fields ? `v.object({\n${fields}\n  })` : 'v.object({})';
    }
    default:
      throw new Error(`Cannot scaffold validator type ${JSON.stringify(descriptor.type)}`);
  }
}

function tableValidatorSource(table: TableDescriptor): string {
  return validatorSource({ type: 'object', shape: table.fields } as JSONValue);
}

export async function createSchemaMigrationScaffold(options: SchemaMigrationScaffoldOptions): Promise<SchemaMigrationScaffoldResult> {
  const slug = migrationSlug(options.name);
  const timestamp = Math.floor((options.now ?? new Date()).getTime() / 1000);
  const id = `${timestamp}_${slug}`;
  const migrationsDir = path.join(options.rootDir, 'pbvex', 'migrations');
  const migrationPath = path.join(migrationsDir, `${id}.ts`);
  if (existsSync(migrationPath)) throw new Error(`Migration already exists: ${path.relative(options.rootDir, migrationPath)}`);

  const sourceTable = options.sourceTable ?? options.targetTable;
  const sourceNote = options.sourceTable
    ? '// Source validator derived from the active deployment manifest.'
    : '// No active deployment was found; source currently mirrors the local target. Adjust it to the real prior schema.';
  const contents = `import { defineMigration } from 'pbvex/server';
import { v } from 'pbvex/values';

${sourceNote}
const from = ${tableValidatorSource(sourceTable)};
const to = ${tableValidatorSource(options.targetTable)};

export default defineMigration({
  id: ${JSON.stringify(id)},
  table: ${JSON.stringify(options.table)},
  mode: 'transactional',
  from,
  to,
  up: (oldDoc) => {
    // TODO: Return a document accepted by the target validator.
    return oldDoc as never;
  },
  down: (newDoc) => {
    // TODO: Return a document accepted by the source validator.
    return newDoc as never;
  },
});
`;
  await mkdir(migrationsDir, { recursive: true });
  await writeFile(migrationPath, contents, { encoding: 'utf8', flag: 'wx' });
  return { migrationPath, usedEmptySource: !options.sourceTable };
}
