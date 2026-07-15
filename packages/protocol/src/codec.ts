import type { JSONValue, JSONObject } from './types.js';
import { validateFieldName, isPlainObject, isSafeFieldName, MAX_VALUE_DEPTH } from './validators.js';
import { parseOpaqueId } from './ids.js';

const MIN_INT64 = BigInt('-9223372036854775808');
const MAX_INT64 = BigInt('9223372036854775807');

export type Id<TableName extends string = string> = string & { __table: TableName };

export type PbvexValue =
  | null
  | boolean
  | number
  | bigint
  | string
  | ArrayBuffer
  | Id
  | PbvexValue[]
  | { [key: string]: undefined | PbvexValue };

export function id<TableName extends string>(_tableName: TableName, value: string): Id<TableName> {
	const parsed = parseOpaqueId(value);
	if (!parsed || parsed.table !== _tableName) throw new Error(`invalid id for table ${_tableName}`);
  return value as Id<TableName>;
}

export function encodeId<TableName extends string>(tableName: TableName, raw: string): Id<TableName> {
	void tableName; void raw;
	throw new Error('backend IDs are authenticated and cannot be created client-side');
}

export function decodeId(value: string): { table: string; raw: string; namespace: string; version: 1 | 2; ok: boolean } {
	const parsed = parseOpaqueId(value);
	return parsed
		? { table: parsed.table, raw: parsed.raw, namespace: parsed.namespace, version: parsed.version, ok: true }
		: { table: '', raw: '', namespace: '', version: 1, ok: false };
}

export function encodeValue(value: PbvexValue): JSONValue {
  return encode(value, new Set<object>(), 0, 'root');
}

export function encodeReturnValue(value: PbvexValue | undefined): JSONValue {
  if (value === undefined) return null;
  return encodeValue(value);
}

function encode(value: unknown, seen: Set<object>, depth: number, path: string): JSONValue {
  if (depth > MAX_VALUE_DEPTH) throw new Error(`Value depth exceeded at ${path}`);
  if (value === undefined) throw new Error(`undefined is not a valid PBVex value at ${path}`);
  if (value === null) return null;
  if (typeof value === 'boolean') return value;
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) throw new Error(`Non-finite number at ${path}`);
    return value;
  }
  if (typeof value === 'string') return value;
  if (typeof value === 'bigint') {
    if (value < MIN_INT64 || value > MAX_INT64) throw new Error(`int64 out of range at ${path}`);
    return { $integer: int64ToBase64(value) };
  }
  if (typeof value === 'object') {
    if (value instanceof ArrayBuffer) {
      return { $bytes: arrayBufferToBase64(value) };
    }
    if (Array.isArray(value)) {
      if (seen.has(value)) throw new Error(`Cyclic reference at ${path}`);
      seen.add(value);
      try {
        return value.map((v, i) => encode(v, seen, depth + 1, `${path}[${i}]`));
      } finally {
        seen.delete(value);
      }
    }
    if (!isPlainObject(value)) {
      const name = Object.getPrototypeOf(value)?.constructor?.name ?? 'unknown';
      throw new Error(`Unsupported object prototype at ${path}: ${name}`);
    }
    if (seen.has(value)) throw new Error(`Cyclic reference at ${path}`);
    seen.add(value);
    try {
      const out: JSONObject = {};
      for (const [k, v] of Object.entries(value)) {
        if (v === undefined) continue;
        validateFieldName(k, path);
        out[k] = encode(v, seen, depth + 1, `${path}.${k}`);
      }
      return out;
    } finally {
      seen.delete(value);
    }
  }
  throw new Error(`Unsupported value at ${path}: ${String(value)}`);
}

export function decodeValue(value: JSONValue): PbvexValue {
  return decode(value, new Set<object>(), 0);
}

function decode(value: JSONValue, seen: Set<object>, depth: number): PbvexValue {
  if (depth > MAX_VALUE_DEPTH) throw new Error('Value depth exceeded in JSON');
  if (value === null || typeof value === 'boolean') {
    return value;
  }
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) throw new Error('Non-finite number cannot be decoded');
    return value;
  }
  if (typeof value === 'string') return value;
  if (typeof value === 'object') {
    if (seen.has(value)) throw new Error('Cyclic reference in JSON');
    seen.add(value);
    try {
      if (Array.isArray(value)) {
        return value.map((v) => decode(v, seen, depth + 1));
      }
      if (!isPlainObject(value)) {
        throw new Error('Unsupported object prototype in JSON');
      }
      const entries = Object.entries(value);
      if (entries.length === 1) {
        const [key, v] = entries[0];
        if (key === '$bytes') {
          if (typeof v !== 'string') throw new Error('Malformed $bytes');
          return base64ToArrayBuffer(v);
        }
        if (key === '$integer') {
          if (typeof v !== 'string') throw new Error('Malformed $integer');
          return base64ToInt64(v);
        }
      }
      const out: Record<string, PbvexValue> = {};
      for (const [k, v] of entries) {
        if (!isSafeFieldName(k)) throw new Error(`Invalid field name in JSON: ${JSON.stringify(k)}`);
        out[k] = decode(v, seen, depth + 1);
      }
      return out;
    } finally {
      seen.delete(value);
    }
  }
  throw new Error('Unexpected JSON value');
}

function int64ToBase64(value: bigint): string {
  const buffer = new ArrayBuffer(8);
  new DataView(buffer).setBigInt64(0, value, true);
  return arrayBufferToBase64(buffer);
}

function base64ToInt64(value: string): bigint {
  const buffer = base64ToArrayBuffer(value);
  if (buffer.byteLength !== 8) throw new Error('Invalid $integer length');
  return new DataView(buffer).getBigInt64(0, true);
}

function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
  let result = '';
  for (let index = 0; index < bytes.length; index += 3) {
    const first = bytes[index]!;
    const second = bytes[index + 1];
    const third = bytes[index + 2];
    result += alphabet[first >> 2];
    result += alphabet[((first & 0x03) << 4) | ((second ?? 0) >> 4)];
    result += second === undefined ? '=' : alphabet[((second & 0x0f) << 2) | ((third ?? 0) >> 6)];
    result += third === undefined ? '=' : alphabet[third & 0x3f];
  }
  return result;
}

function base64ToArrayBuffer(value: string): ArrayBuffer {
  if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value)) {
    throw new Error('Invalid base64 value');
  }
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
  const padding = value.endsWith('==') ? 2 : value.endsWith('=') ? 1 : 0;
  const bytes = new Uint8Array((value.length / 4) * 3 - padding);
  let offset = 0;
  for (let index = 0; index < value.length; index += 4) {
    const a = alphabet.indexOf(value[index]!);
    const b = alphabet.indexOf(value[index + 1]!);
    const c = value[index + 2] === '=' ? 0 : alphabet.indexOf(value[index + 2]!);
    const d = value[index + 3] === '=' ? 0 : alphabet.indexOf(value[index + 3]!);
    bytes[offset++] = (a << 2) | (b >> 4);
    if (offset < bytes.length) bytes[offset++] = ((b & 0x0f) << 4) | (c >> 2);
    if (offset < bytes.length) bytes[offset++] = ((c & 0x03) << 6) | d;
  }
  return bytes.buffer;
}
