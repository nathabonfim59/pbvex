import { describe, expect, it } from 'vitest';
import { canonicalHash } from '@pbvex/protocol';
import { formatMigrationPlan, matchingMigrations, structuralChanges } from '../src/migrations/plan.js';
import type { DeploymentArtifact } from '../src/bundler/manifest.js';

describe('migration planning', () => {
  it('reports structural field changes', () => {
    expect(structuralChanges(
      { tables: [{ tableName: 'users', fields: { name: { type: 'string' } } }] },
      { tables: [{ tableName: 'users', fields: { name: { type: 'string' }, active: { type: 'boolean' } } }] },
    )).toEqual([{ table: 'users', field: 'active', kind: 'field added' }]);
  });

  it('prints empty source and matching discovered migrations without estimates', async () => {
    const targetTable = { tableName: 'users', fields: { active: { type: 'boolean' } } };
    const targetHash = await canonicalHash({ type: 'object', shape: targetTable.fields });
    const artifact = {
      target: 'local', modules: [], bundle: '', sha256: 'a'.repeat(64), size: 0,
      manifest: {
        protocolVersion: 'v1', deploymentId: 'dep_candidate', functions: [], config: {},
        schema: { tables: [targetTable] },
        migrations: [{
          id: '1_users', table: 'users', mode: 'transactional', from: { type: 'object', shape: {} },
          to: { type: 'object', shape: targetTable.fields }, sourceSchemaHash: 'b'.repeat(64),
          targetSchemaHash: targetHash, checksum: 'c'.repeat(64), modulePath: 'pbvex/migrations/1_users.ts',
          exportName: 'default', reversibility: 'reversible',
        }],
      },
    } as DeploymentArtifact;
    const output = await formatMigrationPlan(artifact);
    expect(output).toContain('Source deployment: empty');
    expect(output).toContain('users: table added');
    expect(output).not.toMatch(/count|bytes|estimate/i);
  });

  it('matches a historical multi-step chain to the final table descriptor', async () => {
    const fieldsA = { name: { type: 'string' } };
    const fieldsB = { name: { type: 'string' }, active: { type: 'boolean' } };
    const fieldsC = { name: { type: 'string' }, active: { type: 'boolean' }, role: { type: 'string' } };
    const hashes = await Promise.all([fieldsA, fieldsB, fieldsC].map((shape) => canonicalHash({ type: 'object', shape })));
    const descriptor = (id: string, from: typeof fieldsA, to: typeof fieldsA, sourceSchemaHash: string, targetSchemaHash: string) => ({
      id, table: 'users', mode: 'transactional' as const, from: { type: 'object', shape: from }, to: { type: 'object', shape: to },
      sourceSchemaHash, targetSchemaHash, checksum: id.padEnd(64, '0'), modulePath: `pbvex/migrations/${id}.ts`,
      exportName: 'default', reversibility: 'reversible' as const,
    });
    const first = descriptor('1', fieldsA, fieldsB as typeof fieldsA, hashes[0]!, hashes[1]!);
    const second = descriptor('2', fieldsB as typeof fieldsA, fieldsC as typeof fieldsA, hashes[1]!, hashes[2]!);
    await expect(matchingMigrations(
      { tables: [{ tableName: 'users', fields: fieldsB }] },
      { tables: [{ tableName: 'users', fields: fieldsC }] },
      [first, second],
    )).resolves.toEqual([second]);
  });

  it('rejects stale migration chains that cannot reach the final descriptor', async () => {
    const source = { tables: [{ tableName: 'users', fields: { name: { type: 'string' } } }] };
    const target = { tables: [{ tableName: 'users', fields: { name: { type: 'string' }, active: { type: 'boolean' } } }] };
    const from = await canonicalHash({ type: 'object', shape: source.tables[0]!.fields });
    const staleTo = await canonicalHash({ type: 'object', shape: { stale: { type: 'boolean' } } });
    await expect(matchingMigrations(source, target, [{
      id: 'stale', table: 'users', mode: 'transactional', from: { type: 'object', shape: source.tables[0]!.fields },
      to: { type: 'object', shape: { stale: { type: 'boolean' } } }, sourceSchemaHash: from, targetSchemaHash: staleTo,
      checksum: 'c'.repeat(64), modulePath: 'pbvex/migrations/stale.ts', exportName: 'default', reversibility: 'reversible',
    }])).rejects.toThrow(/does not reach target schema/);
  });
});
