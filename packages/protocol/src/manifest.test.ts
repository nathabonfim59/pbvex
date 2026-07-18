import { test } from 'node:test';
import assert from 'node:assert';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import {
  validateManifest,
  validateUploadRequest,
  validateActivateRequest,
  validateActivateResponse,
  validateDeployment,
  normalizeConfig,
  DEFAULT_CONFIG,
  isValidValidatorDescriptor,
  MAX_DEPLOYMENT_UPLOAD_BYTES,
  MAX_FUNCTION_ARGS_LIMIT,
  MAX_RETURN_VALUE_LIMIT,
} from './manifest.js';
import { canonicalJson, canonicalHash, canonicalHashSync, hashSha256 } from './canonical.js';
import { computeComponentId } from './components.js';
import type { JSONValue } from './types.js';

const fixturesDir = fileURLToPath(new URL('../../../fixtures/manifests', import.meta.url));
const uploadsDir = fileURLToPath(new URL('../../../fixtures/uploads', import.meta.url));

test('image descriptors require bounded predefined thumbs and supported MIME types', () => {
  assert.equal(isValidValidatorDescriptor({ type: 'image', thumbs: ['96x96', '640x0'], mimeTypes: ['image/jpeg', 'image/png'] }), true);
  assert.equal(isValidValidatorDescriptor({ type: 'image', thumbs: ['0x0'], mimeTypes: ['image/png'] }), false);
  assert.equal(isValidValidatorDescriptor({ type: 'image', thumbs: ['640x0f'], mimeTypes: ['image/png'] }), false);
  assert.equal(isValidValidatorDescriptor({ type: 'image', thumbs: ['96x96', '96x96'], mimeTypes: ['image/png'] }), false);
  assert.equal(isValidValidatorDescriptor({ type: 'image', thumbs: ['99999x1'], mimeTypes: ['image/png'] }), false);
  assert.equal(isValidValidatorDescriptor({ type: 'image', thumbs: [], mimeTypes: ['image/svg+xml'] }), false);
});

function fixture(name: string) {
  return readFileSync(`${fixturesDir}/${name}`, 'utf8');
}

function uploadFixture(name: string) {
  return readFileSync(`${uploadsDir}/${name}`, 'utf8');
}

function makeOpaqueID(table: string): string {
  return makeOpaqueIDWithN('test', table);
}

function canonicalJsonString(s: string): string {
  let out = '"';
  for (const ch of s) {
    const code = ch.codePointAt(0)!;
    if (code < 0x80) {
      switch (code) {
        case 0x5c: out += '\\\\'; break;
        case 0x22: out += '\\"'; break;
        case 0x08: out += '\\b'; break;
        case 0x09: out += '\\t'; break;
        case 0x0a: out += '\\n'; break;
        case 0x0c: out += '\\f'; break;
        case 0x0d: out += '\\r'; break;
        case 0x3c: out += '\\u003c'; break;
        case 0x3e: out += '\\u003e'; break;
        case 0x26: out += '\\u0026'; break;
        default:
          if (code < 0x20) out += '\\u' + code.toString(16).padStart(4, '0');
          else out += ch;
      }
    } else if (code === 0x2028) out += '\\u2028';
    else if (code === 0x2029) out += '\\u2029';
    else out += ch;
  }
  return out + '"';
}

function makeOpaqueIDWithN(n: string, table: string): string {
  const payload = `{"v":1,"k":1,"n":${canonicalJsonString(n)},"t":${canonicalJsonString(table)},"r":"abcdefghijklmno"}`;
  return makeOpaqueIDWithPayload(payload);
}

function makeOpaqueIDWithPayload(payload: string): string {
  const bytes = new TextEncoder().encode(payload);
  let binary = '';
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]!);
  const b64 = btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  const mac = btoa(String.fromCharCode(...new Uint8Array(32))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  return `pbv1.1.${b64}.${mac}`;
}

function makeOpaqueIDPadded(table: string): string {
  const valid = makeOpaqueID(table);
  const parts = valid.split('.');
  return `${parts[0]}.${parts[1]}.${parts[2]}=.${parts[3]}`;
}

function makeOpaqueIDInvalidUTF8(): string {
  const invalidBytes = new Uint8Array([0x80]);
  const b64 = btoa(String.fromCharCode(invalidBytes[0]!)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  const mac = btoa(String.fromCharCode(...new Uint8Array(32))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  return `pbv1.1.${b64}.${mac}`;
}

function makeOpaqueIDWithKeyID(keyID: string, table: string): string {
  const payload = `{"v":1,"k":${keyID},"n":"test","t":${canonicalJsonString(table)},"r":"abcdefghijklmno"}`;
  const bytes = new TextEncoder().encode(payload);
  let binary = '';
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]!);
  const b64 = btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  const mac = btoa(String.fromCharCode(...new Uint8Array(32))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  return `pbv1.${keyID}.${b64}.${mac}`;
}

test('isValidValidatorDescriptor opaque ID key ID precision', () => {
  const table = 'trees';
  const validKeyIDs = [
    '1',
    '9007199254740991',
    '9007199254740993',
    '9223372036854775807',
  ];
  for (const keyID of validKeyIDs) {
    assert.ok(
      isValidValidatorDescriptor(
        { type: 'defaulted', validator: { type: 'id', tableName: table }, defaultValue: makeOpaqueIDWithKeyID(keyID, table) },
      ),
      `expected valid opaque ID with keyID=${keyID} to be accepted`,
    );
  }
  const invalidKeyIDs = [
    '9223372036854775808',
    '0',
    '01',
    '-1',
  ];
  for (const keyID of invalidKeyIDs) {
    assert.ok(
      !isValidValidatorDescriptor(
        { type: 'defaulted', validator: { type: 'id', tableName: table }, defaultValue: `pbv1.${keyID}.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA` },
      ),
      `expected invalid opaque ID with keyID=${keyID} to be rejected`,
    );
  }
});

test('isValidValidatorDescriptor opaque ID canonicality', () => {
  const table = 'trees';

  // Valid Go-issued IDs with various n values (non-ASCII, HTML chars, separators)
  const validNs = ['test', 'café', '日本語', '🎉', 'a<b>c', 'a&b', 'a\u2028b', 'a\u2029b', 'a\bb', 'a\fb'];
  for (const n of validNs) {
    const id = makeOpaqueIDWithN(n, table);
    assert.ok(
      isValidValidatorDescriptor(
        { type: 'defaulted', validator: { type: 'id', tableName: table }, defaultValue: id },
      ),
      `expected valid opaque ID with n=${JSON.stringify(n)} to be accepted: ${id}`,
    );
  }

  const invalidIDs: Array<[string, string]> = [
    ['unknown field', makeOpaqueIDWithPayload('{"v":1,"k":1,"n":"test","t":"' + table + '","r":"abcdefghijklmno","extra":1}')],
    ['reordered keys', makeOpaqueIDWithPayload('{"r":"abcdefghijklmno","t":"' + table + '","n":"test","k":1,"v":1}')],
    ['padded payload', makeOpaqueIDPadded(table)],
    ['extra JSON values', makeOpaqueIDWithPayload('{"v":1,"k":1,"n":"test","t":"' + table + '","r":"abcdefghijklmno"}{"extra":1}')],
    ['wrong table target', makeOpaqueIDWithN('test', 'other')],
    ['invalid UTF-8 payload', makeOpaqueIDInvalidUTF8()],
    ['raw < in n (not escaped)', makeOpaqueIDWithPayload('{"v":1,"k":1,"n":"a<b","t":"' + table + '","r":"abcdefghijklmno"}')],
    ['raw U+2028 in n (not escaped)', makeOpaqueIDWithPayload('{"v":1,"k":1,"n":"a\u2028b","t":"' + table + '","r":"abcdefghijklmno"}')],
    ['null payload', makeOpaqueIDWithPayload('null')],
    ['array payload', makeOpaqueIDWithPayload('[1,2,3]')],
    ['string payload', makeOpaqueIDWithPayload('"hello"')],
    ['number payload', makeOpaqueIDWithPayload('42')],
  ];
  for (const [label, id] of invalidIDs) {
    assert.ok(
      !isValidValidatorDescriptor(
        { type: 'defaulted', validator: { type: 'id', tableName: table }, defaultValue: id },
      ),
      `expected invalid opaque ID (${label}) to be rejected: ${id}`,
    );
  }
});

test('isValidValidatorDescriptor any/bare-object default boundary parity', () => {
  const valid: unknown[] = [
    { type: 'defaulted', validator: { type: 'any' }, defaultValue: { ok: 1 } },
    { type: 'defaulted', validator: { type: 'any' }, defaultValue: [1, 2] },
    { type: 'defaulted', validator: { type: 'any' }, defaultValue: 'str' },
    { type: 'defaulted', validator: { type: 'object' }, defaultValue: { ok: 1 } },
  ];
  for (const value of valid) {
    assert.ok(isValidValidatorDescriptor(value), `expected valid: ${JSON.stringify(value)}`);
  }

  const invalid: unknown[] = [
    { type: 'defaulted', validator: { type: 'any' }, defaultValue: { $bad: 0 } },
    { type: 'defaulted', validator: { type: 'any' }, defaultValue: Array(1025).fill(0) },
    { type: 'defaulted', validator: { type: 'any' }, defaultValue: Object.fromEntries(Array(1025).fill(0).map((_, i) => [`k${i}`, 0])) },
    { type: 'defaulted', validator: { type: 'any' }, defaultValue: { $unknown: 'x' } },
    { type: 'defaulted', validator: { type: 'any' }, defaultValue: Infinity },
    { type: 'defaulted', validator: { type: 'object' }, defaultValue: { $bad: 0 } },
    { type: 'defaulted', validator: { type: 'object' }, defaultValue: Array(1025).fill(0) },
  ];
  for (const value of invalid) {
    assert.ok(!isValidValidatorDescriptor(value), `expected invalid: ${JSON.stringify(value)}`);
  }
});

test('validateManifest accepts valid fixtures', () => {
  for (const file of readdirSync(`${fixturesDir}/valid`)) {
    if (!file.endsWith('.json')) continue;
    const raw = readFileSync(`${fixturesDir}/valid/${file}`, 'utf8');
    const parsed = JSON.parse(raw) as unknown;
    const manifest = validateManifest(parsed);
    assert.strictEqual(manifest.protocolVersion, 'v1');
    assert.strictEqual(manifest.deploymentId, (parsed as Record<string, unknown>).deploymentId);
  }
});

test('validateManifest rejects invalid fixtures', () => {
  for (const file of readdirSync(`${fixturesDir}/invalid`)) {
    if (!file.endsWith('.json')) continue;
    const raw = readFileSync(`${fixturesDir}/invalid/${file}`, 'utf8');
    const parsed = JSON.parse(raw) as unknown;
    assert.throws(() => validateManifest(parsed), `Expected ${file} to fail`);
  }
});

test('validateManifest permits schema-only deployments with empty functions', () => {
  const manifest = validateManifest({
    protocolVersion: 'v1',
    deploymentId: 'schema_only',
    functions: [],
  });
  assert.deepStrictEqual(manifest.functions, []);
});

test('validateManifest permits schema-only deployments without functions', () => {
  const manifest = validateManifest({
    protocolVersion: 'v1',
    deploymentId: 'schema_only',
  });
  assert.strictEqual(manifest.functions, undefined);
});

function migrationDescriptor(id = '20260718_add_active') {
  const from = { type: 'object', shape: { name: { type: 'string' } } };
  const to = { type: 'object', shape: { name: { type: 'string' }, active: { type: 'boolean' } } };
  return {
    id,
    table: 'users',
    mode: 'transactional',
    from,
    to,
    sourceSchemaHash: canonicalHashSync(from),
    targetSchemaHash: canonicalHashSync(to),
    checksum: '3'.repeat(64),
    modulePath: `pbvex/migrations/${id}.ts`,
    exportName: 'migration',
    reversibility: 'reversible',
  };
}

test('validateManifest preserves strict migration descriptors', () => {
  const descriptor = migrationDescriptor();
  const manifest = validateManifest({
    protocolVersion: 'v1', deploymentId: 'migration_manifest',
    schema: { tables: [{ tableName: 'users', fields: (descriptor.to as any).shape }] }, migrations: [descriptor],
  });
  assert.deepStrictEqual(manifest.migrations, [descriptor]);
  assert.throws(() => validateManifest({
    protocolVersion: 'v1', deploymentId: 'migration_unknown',
    schema: { tables: [{ tableName: 'users', fields: (descriptor.to as any).shape }] },
    migrations: [{ ...descriptor, unexpected: true }],
  }), /unknown fields/);
});

test('validateManifest rejects duplicate, unsorted, and invalid migrations', () => {
  const manifest = (migrations: unknown[]) => ({
    protocolVersion: 'v1', deploymentId: 'bad_migrations', migrations,
    schema: { tables: [{ tableName: 'users', fields: (migrationDescriptor().to as any).shape }] },
  });
  assert.throws(() => validateManifest(manifest([migrationDescriptor(), migrationDescriptor()])), /duplicated/);
  assert.throws(() => validateManifest(manifest([
    migrationDescriptor('b_second'), migrationDescriptor('a_first'),
  ])), /sorted/);
  for (const descriptor of [
    { ...migrationDescriptor(), id: '../bad' },
    { ...migrationDescriptor(), from: { type: 'bogus' } },
    { ...migrationDescriptor(), from: { type: 'string' } },
    { ...migrationDescriptor(), modulePath: 'pbvex/users.ts' },
    { ...migrationDescriptor(), checksum: 'ABC' },
    { ...migrationDescriptor(), mode: 'background' },
  ]) assert.throws(() => validateManifest(manifest([descriptor])));
});

test('validateActivateResponse preserves bounded Go migration warnings', () => {
  const warning = {
    code: 'transactional_migration_utilization', rows: 8000, rowLimit: 10000,
    estimatedBytes: 1024, byteLimit: 64 << 20, utilizationPercent: 80,
  } as const;
  const response = validateActivateResponse({
    deploymentId: 'dep_warning', activatedAt: '2026-07-18T12:00:00Z', warnings: [warning],
  });
  assert.deepStrictEqual(response.warnings, [warning]);
  for (const invalid of [
    { ...warning, extra: true },
    { ...warning, code: 'other' },
    { ...warning, rows: -1 },
    { ...warning, rowLimit: 10001 },
    { ...warning, estimatedBytes: (64 << 20) + 1 },
    { ...warning, utilizationPercent: 101 },
  ]) {
    assert.throws(() => validateActivateResponse({
      deploymentId: 'dep_warning', activatedAt: '2026-07-18T12:00:00Z', warnings: [invalid],
    }));
  }
  assert.throws(() => validateActivateResponse({
    deploymentId: 'dep_warning', activatedAt: '2026-07-18T12:00:00Z', warnings: [warning, warning],
  }));
});

test('validateManifest reserves generated component physical table names', () => {
  assert.throws(() => validateManifest({
    protocolVersion: 'v1',
    deploymentId: 'reserved_component_physical',
    functions: [],
    schema: { tables: [{ tableName: 'PbVeX_CmP_attacker', fields: {} }] },
  }));
});

test('validateManifest rejects non-array functions', () => {
  assert.throws(() => {
    validateManifest({
      protocolVersion: 'v1',
      deploymentId: 'bad',
      functions: 'not-array',
    } as unknown);
  }, /functions must be an array/);
});

test('validateManifest validates function descriptors', () => {
  const manifest = validateManifest(JSON.parse(fixture('valid/full.json')) as JSONValue);
  const fn = manifest.functions?.find((f) => f.name === 'healthCheck');
  assert.ok(fn);
  assert.strictEqual(fn?.type, 'httpAction');
  assert.strictEqual(fn?.visibility, 'public');
  assert.strictEqual(fn?.modulePath, 'convex/http.ts');
  assert.strictEqual(fn?.exportName, 'healthCheck');
});

test('httpAction route contract is strict and reserves platform paths', () => {
  const validateRoute = (type: string, route?: unknown) => validateManifest({
    protocolVersion: 'v1',
    deploymentId: 'route_contract',
    functions: [{ name: 'handler', type, visibility: 'public', modulePath: 'handler', exportName: 'default', ...(route === undefined ? {} : { route }) }],
  });
  validateRoute('httpAction', { method: 'GET', path: 'health' });
  validateRoute('httpAction', { method: 'DELETE', pathPrefix: 'hooks/' });
  assert.throws(() => validateManifest({
    protocolVersion: 'v1',
    deploymentId: 'internal_http_action',
    functions: [{
      name: 'handler',
      type: 'httpAction',
      visibility: 'internal',
      modulePath: 'handler',
      exportName: 'default',
      route: { method: 'GET', path: 'internal' },
    }],
  }), /httpAction visibility must be public/);
  for (const route of [
    undefined,
    { method: 'get', path: 'health' },
    { method: 'OPTIONS', path: 'health' },
    { method: 'GET', path: '' },
    { method: 'GET', path: 'health', pathPrefix: 'hooks/' },
    { method: 'GET', pathPrefix: 'hooks' },
    { method: 'GET', path: '/health' },
    { method: 'GET', path: 12, pathPrefix: 'hooks/' },
    { method: 'GET', path: 'health', pathPrefix: 12 },
    { method: 'GET', path: 'health', unknown: true },
  ]) assert.throws(() => validateRoute('httpAction', route));
  for (const segment of ['call', 'realtime', 'deployments', 'jobs', 'storage', 'admin']) {
    assert.throws(() => validateRoute('httpAction', { method: 'POST', path: segment }));
    assert.throws(() => validateRoute('httpAction', { method: 'POST', pathPrefix: `${segment}/` }));
  }
  assert.throws(() => validateRoute('query', { method: 'GET', path: 'health' }));
  assert.throws(() => validateRoute('query', null));
});

test('normalizeConfig applies defaults', () => {
  const cfg = normalizeConfig({ maxUploadBytes: 128 });
  assert.strictEqual(cfg.maxUploadBytes, 128);
  assert.strictEqual(cfg.httpPathPrefix, DEFAULT_CONFIG.httpPathPrefix);
});

test('maxUploadBytes accepts the ADR v1 contract ceiling and rejects max+1', () => {
  // The accepted v1 contract (ADR 001) is a 64 MiB upload. The default and the
  // validation ceiling must match the Go backend so a CLI-generated manifest is
  // deployable across both languages.
  assert.strictEqual(MAX_DEPLOYMENT_UPLOAD_BYTES, 64 * 1024 * 1024);
  assert.strictEqual(DEFAULT_CONFIG.maxUploadBytes, MAX_DEPLOYMENT_UPLOAD_BYTES);
  validateManifest({
    protocolVersion: 'v1',
    deploymentId: 'cap_at_max',
    functions: [],
    config: { maxUploadBytes: MAX_DEPLOYMENT_UPLOAD_BYTES },
  });
  assert.throws(() =>
    validateManifest({
      protocolVersion: 'v1',
      deploymentId: 'cap_over_max',
      functions: [],
      config: { maxUploadBytes: MAX_DEPLOYMENT_UPLOAD_BYTES + 1 },
    }),
  );
});

test('canonical manifest hashes are deterministic', async () => {
  const raw = readFileSync(`${fixturesDir}/valid/minimal.json`, 'utf8');
  const manifest = JSON.parse(raw) as JSONValue;
  const hash1 = await canonicalHash(manifest);
  const hash2 = await canonicalHash(JSON.parse(raw) as JSONValue);
  assert.strictEqual(hash1, hash2);
  assert.strictEqual(hash1, await canonicalHash(JSON.parse(canonicalJson(manifest)) as JSONValue));
});

test('every valid manifest fixture is accepted (cross-language parity with Go)', () => {
  // Mirrors backend TestFixtureValidation: the same fixtures must be valid in
  // both the Go and TS protocol implementations.
  for (const file of readdirSync(`${fixturesDir}/valid`)) {
    if (!file.endsWith('.json')) continue;
    const raw = readFileSync(`${fixturesDir}/valid/${file}`, 'utf8');
    validateManifest(JSON.parse(raw) as unknown);
  }
});

test('validateUploadRequest accepts valid upload fixtures', async () => {
  for (const file of readdirSync(`${uploadsDir}/valid`)) {
    if (!file.endsWith('.json')) continue;
    const raw = readFileSync(`${uploadsDir}/valid/${file}`, 'utf8');
    const parsed = JSON.parse(raw) as unknown;
    const upload = await validateUploadRequest(parsed);
    assert.strictEqual(upload.sha256, (parsed as Record<string, unknown>).sha256);
  }
});

test('validateUploadRequest rejects invalid upload fixtures', async () => {
  for (const file of readdirSync(`${uploadsDir}/invalid`)) {
    if (!file.endsWith('.json')) continue;
    const raw = readFileSync(`${uploadsDir}/invalid/${file}`, 'utf8');
    const parsed = JSON.parse(raw) as unknown;
    await assert.rejects(async () => {
      await validateUploadRequest(parsed);
    }, `Expected ${file} to fail`);
  }
});

test('validateUploadRequest rejects hash mismatch', async () => {
  const parsed = JSON.parse(uploadFixture('invalid/bad-hash.json')) as unknown;
  await assert.rejects(async () => {
    await validateUploadRequest(parsed);
  }, /sha256 does not match/);
});

test('validateUploadRequest rejects size mismatch', async () => {
  const parsed = JSON.parse(uploadFixture('invalid/bad-size.json')) as unknown;
  await assert.rejects(async () => {
    await validateUploadRequest(parsed);
  }, /size mismatch/);
});

test('validateActivateRequest requires boolean atomic', () => {
  assert.deepStrictEqual(validateActivateRequest({ atomic: true }), { atomic: true });
  assert.throws(() => validateActivateRequest({ atomic: 'yes' } as unknown), /atomic must be a boolean/);
});

test('validateDeployment validates a stored deployment', () => {
  const deployment = validateDeployment({
    deploymentId: 'd1',
    manifest: JSON.parse(fixture('valid/minimal.json')) as JSONValue,
    bundle: {
      js: 'Y29uc29sZS5sb2coImhlbGxvIik7',
      sha256: '3781f94ea812bb33437de9049e04bc3af41a0e7397164b057379c08c3b0ac489',
      size: 21,
    },
    createdAt: '2024-01-01T00:00:00Z',
    active: false,
  });
  assert.strictEqual(deployment.deploymentId, 'd1');
  assert.strictEqual(deployment.active, false);
});

test('isValidValidatorDescriptor matches the backend deployable contract', () => {
  const valid: unknown[] = [
    { type: 'int64' }, { type: 'float64' }, { type: 'bytes' },
    { type: 'record', key: { type: 'string' }, value: { type: 'int64' } },
    { type: 'record', key: { type: 'literal', value: 'foo' }, value: { type: 'string' } },
    { type: 'record', key: { type: 'union', validators: [{ type: 'string' }, { type: 'literal', value: 'fixed' }] }, value: { type: 'number' } },
    { type: 'literal', value: { $integer: 'AQAAAAAAAAA=' } },
    { type: 'optional', validator: { type: 'bytes' } },
    { type: 'defaulted', validator: { type: 'string' }, defaultValue: 'x' },
    { type: 'defaulted', validator: { type: 'int64' }, defaultValue: { $integer: 'AAAAAAAAAAA=' } },
    { type: 'defaulted', validator: { type: 'bytes' }, defaultValue: { $bytes: 'AA==' } },
    { type: 'defaulted', validator: { type: 'array', item: { type: 'number' } }, defaultValue: [1, 2] },
    { type: 'defaulted', validator: { type: 'object', shape: { a: { type: 'string' } } }, defaultValue: { a: 'x' } },
    { type: 'defaulted', validator: { type: 'null' }, defaultValue: null },
    { type: 'defaulted', validator: { type: 'id', tableName: 'trees' }, defaultValue: makeOpaqueID('trees') },
    {
      type: 'recursive', name: 'Node',
      validator: {
        type: 'object', shape: {
          label: { type: 'string' },
          children: { type: 'array', item: { type: 'ref', name: 'Node' } },
        },
      },
    },
  ];
  for (const value of valid) {
    assert.ok(isValidValidatorDescriptor(value), `expected valid descriptor ${JSON.stringify(value)}`);
  }

  const invalid: unknown[] = [
    // delayed is never a deployable descriptor.
    { type: 'delayed' },
    { type: 'defaulted', validator: { type: 'string' } },
    { type: 'defaulted', validator: { type: 'string' }, defaultValue: 'x', extra: 1 },
    { type: 'defaulted', validator: { type: 'wat' }, defaultValue: 'x' },
    { type: 'defaulted', validator: { type: 'string' }, defaultValue: { $integer: 'short' } },
    { type: 'defaulted', validator: { type: 'string' }, defaultValue: { $unknown: 'x' } },
    // defaulted default value must validate against the child validator.
    { type: 'defaulted', validator: { type: 'string' }, defaultValue: 42 },
    { type: 'defaulted', validator: { type: 'number' }, defaultValue: 'not-a-number' },
    { type: 'defaulted', validator: { type: 'boolean' }, defaultValue: 1 },
    { type: 'defaulted', validator: { type: 'int64' }, defaultValue: 'not-a-wire-integer' },
    { type: 'defaulted', validator: { type: 'array', item: { type: 'number' } }, defaultValue: [1, 'x'] },
    { type: 'defaulted', validator: { type: 'object', shape: { a: { type: 'string' } } }, defaultValue: { a: 123 } },
    // id defaults must be structurally valid opaque IDs for the declared table.
    { type: 'defaulted', validator: { type: 'id', tableName: 'trees' }, defaultValue: 'not-an-id' },
    { type: 'defaulted', validator: { type: 'id', tableName: 'trees' }, defaultValue: 'pbv1.1.bad.bad' },
    // array defaults above the collection cap.
    { type: 'defaulted', validator: { type: 'array', item: { type: 'number' } }, defaultValue: Array(1025).fill(0) },
    // record defaults above the collection cap or with unsafe wire keys.
    { type: 'defaulted', validator: { type: 'record', key: { type: 'string' }, value: { type: 'number' } }, defaultValue: Object.fromEntries(Array(1025).fill(0).map((_, i) => [`k${i}`, 0])) },
    { type: 'defaulted', validator: { type: 'record', key: { type: 'string' }, value: { type: 'number' } }, defaultValue: { '$bad': 0 } },
    // bare recursive marker has no target identity and is rejected.
    { type: 'recursive' },
    { type: 'recursive', name: 'Node' },
    { type: 'recursive', name: '1bad', validator: { type: 'string' } },
    { type: 'recursive', name: 'Node', validator: { type: 'string' }, extra: 1 },
    // ref to an undeclared name is rejected at descriptor validation time.
    { type: 'ref', name: 'Missing' },
    { type: 'ref', name: 'Node', extra: 1 },
    // record key must be string, string-literal, or union of those.
    { type: 'record', value: { type: 'string' } },
    { type: 'record', key: { type: 'string' }, value: { type: 'wat' } },
    { type: 'record', key: { type: 'number' }, value: { type: 'string' } },
    { type: 'record', key: { type: 'boolean' }, value: { type: 'string' } },
    { type: 'record', key: { type: 'literal', value: 42 }, value: { type: 'string' } },
    { type: 'record', key: { type: 'object', shape: {} }, value: { type: 'string' } },
    // object with both shape and fields is rejected (Go len(o)!=2).
    { type: 'object', shape: { a: { type: 'string' } }, fields: { a: { type: 'string' } } },
    // union above the 64-member cap.
    { type: 'union', validators: Array(65).fill({ type: 'string' }) },
    { type: 'bogus' },
    { validate: () => 1 },
    null,
    'string',
  ];
  for (const value of invalid) {
    assert.ok(!isValidValidatorDescriptor(value), `expected invalid descriptor ${JSON.stringify(value)}`);
  }
});

test('validateManifest accepts a schema with defaulted and recursive descriptors end-to-end', () => {
  const manifest = validateManifest({
    protocolVersion: 'v1',
    deploymentId: 'dep1',
    schema: {
      tables: [
        {
          tableName: 'nodes',
          fields: {
            name: { type: 'string' },
            count: { type: 'defaulted', validator: { type: 'number' }, defaultValue: 0 },
            priority: { type: 'defaulted', validator: { type: 'int64' }, defaultValue: { $integer: 'AAAAAAAAAAA=' } },
            children: {
              type: 'recursive', name: 'Node',
              validator: {
                type: 'object', shape: {
                  name: { type: 'string' },
                  children: { type: 'array', item: { type: 'ref', name: 'Node' } },
                },
              },
            },
          },
        },
      ],
    },
  } as JSONValue);
  assert.ok(manifest.schema);
  assert.strictEqual(manifest.schema!.tables[0]!.tableName, 'nodes');
});

test('validateFunctionDescriptor applies the descriptor contract to args/returns', () => {
  // A function whose args are a recursive type (named refs) is accepted.
  const tree = {
    type: 'recursive', name: 'Node',
    validator: {
      type: 'object', shape: {
        name: { type: 'string' },
        children: { type: 'array', item: { type: 'ref', name: 'Node' } },
      },
    },
  };
  const manifest = validateManifest({
    protocolVersion: 'v1',
    deploymentId: 'recursive_fn',
    functions: [
      { name: 'grow', type: 'mutation', visibility: 'public', modulePath: 'tree', exportName: 'default', args: tree, returns: { type: 'id', tableName: 'trees' } },
    ],
  } as JSONValue);
  assert.strictEqual(manifest.functions![0]!.name, 'grow');

  // Malformed recursive function args are rejected.
  const malformed = [
    // bare recursive marker (no target identity)
    { type: 'recursive' },
    // ref to an undeclared name
    { type: 'ref', name: 'Missing' },
    // recursive whose inner references an undeclared name
    { type: 'recursive', name: 'Node', validator: { type: 'object', shape: { x: { type: 'ref', name: 'Other' } } } },
    // unknown descriptor type
    { type: 'bogus' },
  ];
  for (const args of malformed) {
    assert.throws(() =>
      validateManifest({
        protocolVersion: 'v1',
        deploymentId: 'recursive_fn_bad',
        functions: [
          { name: 'bad', type: 'mutation', visibility: 'public', modulePath: 'tree', exportName: 'default', args, returns: { type: 'null' } },
        ],
      } as JSONValue),
    );
  }
});

test('isValidValidatorDescriptor validates recursive defaulted default values', () => {
  const valid = {
    type: 'recursive', name: 'Node',
    validator: {
      type: 'object', shape: {
        name: { type: 'string' },
        fallback: {
          type: 'optional', validator: {
            type: 'defaulted',
            validator: { type: 'ref', name: 'Node' },
            defaultValue: { name: 'seed', children: [] },
          },
        },
        children: { type: 'array', item: { type: 'ref', name: 'Node' } },
      },
    },
  };
  assert.ok(isValidValidatorDescriptor(valid));

  const invalid = {
    type: 'recursive', name: 'Node',
    validator: {
      type: 'object', shape: {
        name: { type: 'string' },
        fallback: {
          type: 'optional', validator: {
            type: 'defaulted',
            validator: { type: 'ref', name: 'Node' },
            defaultValue: { name: 123, children: [] },
          },
        },
        children: { type: 'array', item: { type: 'ref', name: 'Node' } },
      },
    },
  };
  assert.ok(!isValidValidatorDescriptor(invalid));
});

test('isValidValidatorDescriptor depth boundary parity', () => {
  let d: unknown = { type: 'string' };
  for (let i = 0; i < 128; i++) d = { type: 'optional', validator: d };
  assert.ok(isValidValidatorDescriptor(d), '128 optional wrappers (depth 0..128) should be valid');
  d = { type: 'string' };
  for (let i = 0; i < 129; i++) d = { type: 'optional', validator: d };
  assert.ok(!isValidValidatorDescriptor(d), '129 optional wrappers (depth 129) should be invalid');
});

test('isValidValidatorDescriptor node budget boundary parity', () => {
  function makeWide(outer: number, inner: number): unknown {
    const shape: Record<string, unknown> = {};
    for (let i = 0; i < outer; i++) {
      const innerShape: Record<string, unknown> = {};
      for (let j = 0; j < inner; j++) innerShape[`f${j}`] = { type: 'string' };
      shape[`g${i}`] = { type: 'object', shape: innerShape };
    }
    return { type: 'object', shape };
  }
  // 1 (outer) + 127 (inner objects) + 127*128 (strings) = 16384 nodes
  assert.ok(isValidValidatorDescriptor(makeWide(127, 128)), '16384 descriptor nodes should be valid');
  // 1 + 128 + 128*128 = 16513 nodes → exceeds 16384
  assert.ok(!isValidValidatorDescriptor(makeWide(128, 128)), '16513 descriptor nodes should be invalid');
});

test('isValidValidatorDescriptor byte budget boundary parity', () => {
  const ok = 'x'.repeat(4 << 20);
  assert.ok(
    isValidValidatorDescriptor({ type: 'defaulted', validator: { type: 'string' }, defaultValue: ok }),
    '4MiB string default should be valid',
  );
  const bad = 'x'.repeat((4 << 20) + 1);
  assert.ok(
    !isValidValidatorDescriptor({ type: 'defaulted', validator: { type: 'string' }, defaultValue: bad }),
    '4MiB+1 string default should be invalid',
  );
});

test('validateManifest rejects maxFunctionArgsBytes above 16 MiB ceiling', () => {
  const base = JSON.parse(fixture('valid/minimal.json'));
  base.config = { maxFunctionArgsBytes: MAX_FUNCTION_ARGS_LIMIT };
  validateManifest(base);
  base.config = { maxFunctionArgsBytes: MAX_FUNCTION_ARGS_LIMIT + 1 };
  assert.throws(() => validateManifest(base), /maxFunctionArgsBytes/);
});

test('validateManifest rejects maxReturnValueBytes above 16 MiB ceiling', () => {
  const base = JSON.parse(fixture('valid/minimal.json'));
  base.config = { maxReturnValueBytes: MAX_RETURN_VALUE_LIMIT };
  validateManifest(base);
  base.config = { maxReturnValueBytes: MAX_RETURN_VALUE_LIMIT + 1 };
  assert.throws(() => validateManifest(base), /maxReturnValueBytes/);
});

async function componentUpload(overrides: Record<string, unknown> = {}): Promise<Record<string, unknown>> {
  const code = 'export const x = 1;';
  const bundleSha = await hashSha256(code);
  const definition = {
    componentId: '',
    modulePaths: ['store.ts'],
    moduleHashes: { 'store.ts': await hashSha256(code) },
    dependencies: [],
    schema: { tables: [{ tableName: 'nums', fields: { value: { type: 'number' } } }] },
  };
  definition.componentId = await computeComponentId(definition, bundleSha);
  const bytes = Buffer.from(code).toString('base64');
  return {
    manifest: {
      protocolVersion: 'v1',
      deploymentId: 'component_upload',
      functions: [{ name: 'add', type: 'mutation', visibility: 'public', modulePath: 'pbvex/components/counter/store.ts', exportName: 'add' }],
      components: { definitions: [definition], mounts: [{ name: 'counter', componentId: definition.componentId }] },
    },
    bundle: bytes,
    sha256: bundleSha,
    size: Buffer.byteLength(code),
    modules: [{ path: 'pbvex/components/counter/store.ts', bytes }],
    ...overrides,
  };
}

test('component upload authenticates source modules and content identity', async () => {
  await validateUploadRequest(await componentUpload());
});

test('component upload rejects tampered source bytes', async () => {
  const request = await componentUpload({ modules: [{ path: 'pbvex/components/counter/store.ts', bytes: Buffer.from('evil').toString('base64') }] });
  await assert.rejects(validateUploadRequest(request), /hash mismatch/);
});

test('component upload rejects tampered content identity', async () => {
  const request = await componentUpload();
  const graph = (request.manifest as any).components;
  const tampered = `${graph.definitions[0].componentId.slice(0, -1)}0`;
  graph.definitions[0].componentId = tampered;
  graph.mounts[0].componentId = tampered;
  await assert.rejects(validateUploadRequest(request), /content hash/);
});

test('component upload requires authenticated source modules', async () => {
  const request = await componentUpload();
  delete request.modules;
  await assert.rejects(validateUploadRequest(request), /modules are required/);
});

test('component upload rejects executable bundle swaps', async () => {
  const request = await componentUpload();
  const evil = 'export const evil = true;';
  request.bundle = Buffer.from(evil).toString('base64');
  request.sha256 = await hashSha256(evil);
  request.size = Buffer.byteLength(evil);
  await assert.rejects(validateUploadRequest(request), /content hash/);
});

test('component env declarations reject coercible non-string values', () => {
  assert.throws(() => validateManifest({
    protocolVersion: 'v1', deploymentId: 'env_contract', functions: [],
    components: {
      definitions: [{
        componentId: `def_${'0'.repeat(64)}`,
        modulePaths: ['store.ts'], moduleHashes: { 'store.ts': '0'.repeat(64) },
        env: { TOKEN: { type: 'value', value: 123 } },
      }],
      mounts: [],
    },
  }), /env is invalid/);
});

test('component mount preserves omitted args and rejects explicit args for no-args definitions', () => {
  const definition = { componentId: `def_${'0'.repeat(64)}`, modulePaths: ['store.ts'], moduleHashes: { 'store.ts': '0'.repeat(64) } };
  const manifest = (def: object, mount: object) => ({
    protocolVersion: 'v1', deploymentId: 'component_args', functions: [],
    components: { definitions: [def], mounts: [mount] },
  });
  assert.doesNotThrow(() => validateManifest(manifest({
    ...definition,
    args: { type: 'object', shape: { label: { type: 'optional', validator: { type: 'string' } }, count: { type: 'defaulted', validator: { type: 'number' }, defaultValue: 1 } } },
  }, { name: 'counter', componentId: definition.componentId })));
  assert.throws(() => validateManifest(manifest(definition, { name: 'counter', componentId: definition.componentId, args: null })), /does not accept args/);
});

test('stored component deployment rejects tampered identity', async () => {
  const request = await componentUpload();
  const graph = (request.manifest as any).components;
  const tampered = `${graph.definitions[0].componentId.slice(0, -1)}0`;
  graph.definitions[0].componentId = tampered;
  graph.mounts[0].componentId = tampered;
  assert.throws(() => validateDeployment({
    deploymentId: 'stored', manifest: request.manifest,
    bundle: { js: request.bundle, sha256: request.sha256, size: request.size },
    createdAt: '2024-01-01T00:00:00Z', active: false,
  }), /content hash/);
});

test('stored component deployment accepts authenticated identity', async () => {
  const request = await componentUpload();
  const deployment = validateDeployment({
    deploymentId: 'stored', manifest: request.manifest,
    bundle: { js: request.bundle, sha256: request.sha256, size: request.size },
    createdAt: '2024-01-01T00:00:00Z', active: false,
  });
  assert.strictEqual(deployment.deploymentId, 'stored');
});
