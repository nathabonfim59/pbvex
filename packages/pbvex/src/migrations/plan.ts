import { readFile } from 'node:fs/promises';
import type { Deployment, DeploymentManifest, DeploymentUploadRequest, JSONValue, MigrationDescriptor, SchemaDescriptor, TableDescriptor } from '@pbvex/protocol';
import { canonicalHash, validateUploadRequest } from '@pbvex/protocol';
import type { DeploymentArtifact } from '../bundler/manifest.js';
import { canonicalSchemaHash } from '../bundler/manifest.js';

export interface MigrationSource {
  deploymentId?: string;
  bundleHash?: string;
  manifest: DeploymentManifest;
}

export async function readActiveArtifact(file: string): Promise<MigrationSource> {
  let parsed: unknown;
  try { parsed = JSON.parse(await readFile(file, 'utf8')); }
  catch (error) { throw new Error(`Invalid active artifact: ${error instanceof Error ? error.message : String(error)}`); }
  let request: DeploymentUploadRequest;
  try { request = await validateUploadRequest(parsed); }
  catch (error) { throw new Error(`Invalid active artifact: ${error instanceof Error ? error.message : String(error)}`); }
  return { deploymentId: request.manifest.deploymentId, bundleHash: request.sha256, manifest: request.manifest };
}

export function activeDeployment(deployments: Deployment[]): MigrationSource | undefined {
  const active = deployments.find((deployment) => deployment.active);
  return active ? { deploymentId: active.deploymentId, bundleHash: active.bundle.sha256, manifest: active.manifest } : undefined;
}

function byName(schema?: SchemaDescriptor): Map<string, TableDescriptor> {
  return new Map((schema?.tables ?? []).map((table) => [table.tableName, table]));
}

function stable(value: unknown): string { return JSON.stringify(value); }

export interface StructuralChange {
  table: string;
  kind: 'table added' | 'table removed' | 'field added' | 'field removed' | 'field changed' | 'indexes changed';
  field?: string;
}

export function structuralChanges(source?: SchemaDescriptor, target?: SchemaDescriptor): StructuralChange[] {
  const oldTables = byName(source);
  const newTables = byName(target);
  const changes: StructuralChange[] = [];
  for (const name of Array.from(new Set([...oldTables.keys(), ...newTables.keys()])).sort()) {
    const oldTable = oldTables.get(name);
    const newTable = newTables.get(name);
    if (!oldTable) { changes.push({ table: name, kind: 'table added' }); continue; }
    if (!newTable) { changes.push({ table: name, kind: 'table removed' }); continue; }
    for (const field of Array.from(new Set([...Object.keys(oldTable.fields), ...Object.keys(newTable.fields)])).sort()) {
      if (!(field in oldTable.fields)) changes.push({ table: name, field, kind: 'field added' });
      else if (!(field in newTable.fields)) changes.push({ table: name, field, kind: 'field removed' });
      else if (stable(oldTable.fields[field]) !== stable(newTable.fields[field])) changes.push({ table: name, field, kind: 'field changed' });
    }
    if (stable(oldTable.indexes ?? []) !== stable(newTable.indexes ?? [])) changes.push({ table: name, kind: 'indexes changed' });
  }
  return changes;
}

async function tableHash(table: TableDescriptor): Promise<string> {
  return canonicalHash({ type: 'object', shape: table.fields } as JSONValue);
}

export async function matchingMigrations(
  source: SchemaDescriptor | undefined,
  target: SchemaDescriptor | undefined,
  migrations: MigrationDescriptor[] = [],
): Promise<MigrationDescriptor[]> {
  const oldTables = byName(source);
  const newTables = byName(target);
  const matches: MigrationDescriptor[] = [];
  for (const [table, newTable] of newTables) {
    const candidates = migrations.filter((migration) => migration.table === table);
    if (!candidates.length) continue;
    const goal = await tableHash(newTable);
    const bySource = new Map<string, MigrationDescriptor>();
    for (const migration of candidates) {
      if (bySource.has(migration.sourceSchemaHash)) throw new Error(`Ambiguous migration chain for table ${table}`);
      bySource.set(migration.sourceSchemaHash, migration);
    }
    for (const migration of candidates) {
      const seen = new Set<string>();
      let hash = migration.targetSchemaHash;
      while (hash !== goal) {
        if (seen.has(hash)) throw new Error(`Cyclic migration chain for table ${table}`);
        seen.add(hash);
        const next = bySource.get(hash);
        if (!next) throw new Error(`Migration chain for table ${table} does not reach target schema`);
        hash = next.targetSchemaHash;
      }
    }
    const oldTable = oldTables.get(table);
    if (!oldTable) continue;
    let current = await tableHash(oldTable);
    const seen = new Set<string>();
    while (current !== goal) {
      if (seen.has(current)) throw new Error(`Cyclic migration chain for table ${table}`);
      seen.add(current);
      const next = bySource.get(current);
      if (!next) throw new Error(`Migration chain from active ${table} schema does not reach target schema`);
      matches.push(next);
      current = next.targetSchemaHash;
    }
  }
  return matches;
}

export async function formatMigrationPlan(candidate: DeploymentArtifact, source?: MigrationSource): Promise<string> {
  const sourceSchema = source?.manifest.schema;
  const targetSchema = candidate.manifest.schema;
  const changes = structuralChanges(sourceSchema, targetSchema);
  const matches = await matchingMigrations(sourceSchema, targetSchema, candidate.manifest.migrations);
  const lines = [
    `Source deployment: ${source?.deploymentId ?? 'empty'}`,
    `Source deployment hash: ${source?.bundleHash ?? 'empty'}`,
    `Source schema hash: ${await canonicalSchemaHash(sourceSchema)}`,
    `Target deployment: ${candidate.manifest.deploymentId}`,
    `Target deployment hash: ${candidate.sha256}`,
    `Target schema hash: ${await canonicalSchemaHash(targetSchema)}`,
    '',
    'Structural changes:',
    ...(changes.length ? changes.map((change) => `  - ${change.table}${change.field ? `.${change.field}` : ''}: ${change.kind}`) : ['  (none)']),
    '',
    'Discovered migration matches:',
    ...(matches.length ? matches.map((migration) => `  - ${migration.id} (${migration.table}, ${migration.mode})`) : ['  (none)']),
  ];
  return lines.join('\n');
}
