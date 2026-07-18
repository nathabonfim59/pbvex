import type { JSONValue, FunctionType, FunctionVisibility } from './types.js';
import { parseOpaqueId } from './ids.js';

export const MAX_IDENTIFIER_LENGTH = 1024;
export const MAX_PATH_LENGTH = 4096;
export const MAX_FIELD_LENGTH = 1024;
export const MAX_VALUE_DEPTH = 128;
export const SHA256_HEX_LENGTH = 64;

const IDENTIFIER_RE = /^[a-zA-Z][a-zA-Z0-9_]*$/;
const MODULE_PATH_RE = /^[a-zA-Z0-9._/\-]+$/;
const COMPONENT_ID_RE = /^[a-zA-Z][a-zA-Z0-9_-]*$/;
const COMPONENT_RELATIVE_MODULE_RE = /^[a-zA-Z][a-zA-Z0-9_./\-]*\.ts$/;
const SHA256_HEX_RE = /^[0-9a-f]{64}$/;
const RESERVED_KEYS = new Set(['__proto__', 'constructor', 'prototype']);
const NATIVE_OBJECT_SOURCE = Function.prototype.toString.call(Object);

/**
 * Reports whether a value is an ordinary record, including records created in
 * another JavaScript realm. Cross-realm Object.prototype identities differ,
 * so prototype identity alone is insufficient; a genuine Object prototype is
 * itself rooted at null and owns the Object constructor.
 */
export function isPlainObject(value: unknown): value is Record<string, unknown> {
  if (typeof value !== 'object' || value === null) return false;
  const proto = Object.getPrototypeOf(value);
  if (proto === null || proto === Object.prototype) return true;
  if (Object.getPrototypeOf(proto) !== null) return false;
  const constructor = Object.getOwnPropertyDescriptor(proto, 'constructor')?.value;
  if (typeof constructor !== 'function' || constructor.name !== 'Object') return false;
  try {
    return Function.prototype.toString.call(constructor) === NATIVE_OBJECT_SOURCE;
  } catch {
    return false;
  }
}

export function isIdentifier(value: unknown): value is string {
  return (
    typeof value === 'string' &&
    value.length > 0 &&
    value.length <= MAX_IDENTIFIER_LENGTH &&
    IDENTIFIER_RE.test(value)
  );
}

export function isModulePath(value: unknown): value is string {
  return (
    typeof value === 'string' &&
    value.length > 0 &&
    value.length <= MAX_PATH_LENGTH &&
    !value.startsWith('/') &&
    !value.includes('..') &&
    MODULE_PATH_RE.test(value)
  );
}

export function isComponentId(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.length <= MAX_IDENTIFIER_LENGTH && COMPONENT_ID_RE.test(value);
}

export function isComponentRelativeModulePath(value: unknown): value is string {
  if (typeof value !== 'string' || value.length === 0 || value.length > MAX_PATH_LENGTH) return false;
  if (value.startsWith('/') || value.startsWith('\\') || value.includes('..') || value.includes(':')) return false;
  return COMPONENT_RELATIVE_MODULE_RE.test(value);
}

export function isFunctionType(value: unknown): value is FunctionType {
  return value === 'query' || value === 'mutation' || value === 'action' || value === 'httpAction';
}

export function isFunctionVisibility(value: unknown): value is FunctionVisibility {
  return value === 'public' || value === 'internal';
}

export function isExportName(value: unknown): value is 'default' | string {
  if (typeof value !== 'string' || value.length === 0) return false;
  return value === 'default' || isIdentifier(value);
}

export function isSha256Hex(value: unknown): value is string {
  return typeof value === 'string' && SHA256_HEX_RE.test(value);
}

export function isNonNegativeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0;
}

export function isBase64String(value: unknown): value is string {
  if (typeof value !== 'string' || value.length === 0) return false;
  // Canonical standard base64: correct alphabet, correct padding length,
  // and canonical encoding (round-trip matches). Mirrors Go's
  // base64.StdEncoding which rejects unpadded/non-standard base64.
  if (value.length % 4 !== 0) return false;
  if (!/^[A-Za-z0-9+/]*={0,2}$/.test(value)) return false;
  try {
    return btoa(atob(value)) === value;
  } catch {
    return false;
  }
}

export function isSafeFieldName(key: string): boolean {
  return (
    key.length > 0 &&
    key.length <= MAX_FIELD_LENGTH &&
    !key.startsWith('$') &&
    !RESERVED_KEYS.has(key) &&
    isAsciiPrintable(key)
  );
}

function isAsciiPrintable(key: string): boolean {
  for (let i = 0; i < key.length; i += 1) {
    const code = key.charCodeAt(i);
    if (code < 32 || code >= 127) return false;
  }
  return true;
}

export function isJsonValue(value: unknown): value is JSONValue {
  return isJsonValueInternal(value, 0, new Set());
}

function isJsonValueInternal(value: unknown, depth: number, seen: Set<object>): boolean {
  if (depth > MAX_VALUE_DEPTH) return false;
  if (value === null || typeof value === 'boolean' || typeof value === 'number' || typeof value === 'string') {
    if (typeof value === 'number' && !Number.isFinite(value)) return false;
    return true;
  }
  if (typeof value !== 'object') return false;
  if (seen.has(value)) return false;
  seen.add(value);
  try {
    if (Array.isArray(value)) {
      return value.every((v) => isJsonValueInternal(v, depth + 1, seen));
    }
    if (!isPlainObject(value)) return false;
    for (const v of Object.values(value)) {
      if (!isJsonValueInternal(v, depth + 1, seen)) {
        return false;
      }
    }
    return true;
  } finally {
    seen.delete(value);
  }
}

export function validateFieldName(key: string, path: string): void {
  if (!isSafeFieldName(key)) {
    throw new Error(`Invalid object field name at ${path}: ${JSON.stringify(key)}`);
  }
}

export function isIsoDateString(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && !Number.isNaN(Date.parse(value));
}

// hasOnlyKeys reports whether an object has no keys outside the allow-list.
// It mirrors deploy.onlyKeys in the Go validator for cross-language parity.
export function hasOnlyKeys(value: object, allowed: string[]): boolean {
  const allow = new Set(allowed);
  for (const key of Object.keys(value)) {
    if (!allow.has(key)) return false;
  }
  return true;
}

const SIMPLE_VALIDATOR_TYPES = new Set([
  'string',
  'number',
  'int64',
  'float64',
  'bytes',
  'boolean',
  'any',
  'null',
]);

// isValidatorDescriptor reports whether a value is a syntactically valid
// validator descriptor. It mirrors deploy.validateValidatorDescriptor in the Go
// validator so both sides agree on what may appear in component args / schema
// field descriptors.
export function isValidatorDescriptor(value: unknown): boolean {
  return isValidatorDescriptorAt(value, 0);
}

function isValidatorDescriptorAt(value: unknown, depth: number): boolean {
  if (depth > MAX_VALUE_DEPTH) return false;
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false;
  const o = value as Record<string, unknown>;
  const typeName = o.type;
  if (typeof typeName !== 'string') return false;

  if (SIMPLE_VALIDATOR_TYPES.has(typeName)) {
    return Object.keys(o).length === 1;
  }
  switch (typeName) {
    case 'id': {
      return (
        Object.keys(o).length === 2 &&
        typeof o.tableName === 'string' &&
        isIdentifier(o.tableName)
      );
    }
    case 'image':
      return validImageDescriptor(o);
    case 'literal': {
      return Object.keys(o).length === 2 && 'value' in o && isLiteralValue(o.value, depth);
    }
    case 'array': {
      return Object.keys(o).length === 2 && isValidatorDescriptorAt(o.item, depth + 1);
    }
    case 'record': {
      return (
        Object.keys(o).length === 3 &&
        isValidatorDescriptorAt(o.key, depth + 1) &&
        isValidatorDescriptorAt(o.value, depth + 1)
      );
    }
    case 'object': {
      if (Object.keys(o).length !== 2) return false;
      const shape = o.shape;
      if (typeof shape !== 'object' || shape === null || Array.isArray(shape)) return false;
      for (const [k, v] of Object.entries(shape as Record<string, unknown>)) {
        if (!isSafeFieldName(k) || !isValidatorDescriptorAt(v, depth + 1)) return false;
      }
      return true;
    }
    case 'union': {
      if (Object.keys(o).length !== 2) return false;
      const validators = o.validators;
      if (!Array.isArray(validators) || validators.length === 0) return false;
      return validators.every((v) => isValidatorDescriptorAt(v, depth + 1));
    }
    case 'optional': {
      return Object.keys(o).length === 2 && isValidatorDescriptorAt(o.validator, depth + 1);
    }
    case 'defaulted': {
      if (Object.keys(o).length !== 3 || !('validator' in o) || !('defaultValue' in o)) return false;
      if (!isValidatorDescriptorAt(o.validator, depth + 1)) return false;
      return validateValueAt(o.validator as JSONValue, o.defaultValue, depth + 1, true);
    }
    default:
      return false;
  }
}

function isLiteralValue(value: unknown, depth: number): boolean {
  if (depth > MAX_VALUE_DEPTH) return false;
  if (value === null || typeof value === 'boolean' || typeof value === 'number' || typeof value === 'string') {
    return true;
  }
  if (Array.isArray(value)) {
    return value.every((v) => isLiteralValue(v, depth + 1));
  }
  if (typeof value === 'object') {
    const o = value as Record<string, unknown>;
    const keys = Object.keys(o);
    if (keys.length === 1) {
      const k = keys[0];
      const v = o[k];
      if (k === '$integer' && typeof v === 'string') {
        return isValidInt64Base64(v);
      }
      if (k === '$bytes' && typeof v === 'string') {
        return isValidBase64(v);
      }
    }
    for (const [k, v] of Object.entries(o)) {
      if (!isSafeFieldName(k) || !isLiteralValue(v, depth + 1)) return false;
    }
    return true;
  }
  return false;
}

function isValidInt64Base64(value: string): boolean {
  try {
    const binary = atob(value);
    return binary.length === 8 && btoa(binary) === value;
  } catch {
    return false;
  }
}

function isValidBase64(value: string): boolean {
  try {
    return btoa(atob(value)) === value;
  } catch {
    return false;
  }
}

// validateValue reports whether a JSON value satisfies a validator descriptor.
// It mirrors values.ValidateValue in Go so both sides agree on mount-args /
// schema-field validation. A nil/undefined descriptor matches any value.
export function validateValue(descriptor: JSONValue | undefined, value: unknown, allowLegacyRoot = true): boolean {
  return validateValueAt(descriptor, value, 0, allowLegacyRoot);
}

function validateValueAt(descriptor: JSONValue | undefined, value: unknown, depth: number, allowLegacyRoot: boolean): boolean {
  if (depth > MAX_VALUE_DEPTH) return false;
  if (descriptor === undefined || descriptor === null) return true;
  if (typeof descriptor !== 'object' || Array.isArray(descriptor)) return false;
  const o = descriptor as Record<string, unknown>;
  const typ = o.type;
  if (typeof typ !== 'string') return false;
  if (typ === 'any') return Object.keys(o).length === 1;
  if (typ === 'optional') {
    return Object.keys(o).length === 2 && validateValueAt(o.validator as JSONValue, value, depth + 1, allowLegacyRoot);
  }
  if (typ === 'defaulted') {
    if (Object.keys(o).length !== 3) return false;
    return validateValueAt(o.validator as JSONValue, value, depth + 1, allowLegacyRoot);
  }
  switch (typ) {
    case 'string':
      return Object.keys(o).length === 1 && typeof value === 'string';
    case 'number':
    case 'float64':
      if (Object.keys(o).length !== 1) return false;
      return typeof value === 'number' && Number.isFinite(value);
    case 'int64': {
      if (Object.keys(o).length !== 1) return false;
      if (typeof value !== 'object' || value === null || Array.isArray(value)) return false;
      const m = value as Record<string, unknown>;
      const s = m.$integer;
      if (typeof s !== 'string' || Object.keys(m).length !== 1) return false;
      try {
        const binary = atob(s);
        return binary.length === 8 && btoa(binary) === s;
      } catch {
        return false;
      }
    }
    case 'boolean':
      return Object.keys(o).length === 1 && typeof value === 'boolean';
    case 'null':
      return Object.keys(o).length === 1 && value === null;
    case 'bytes': {
      if (Object.keys(o).length !== 1) return false;
      if (typeof value !== 'object' || value === null || Array.isArray(value)) return false;
      const m = value as Record<string, unknown>;
      if (typeof m.$bytes !== 'string' || Object.keys(m).length !== 1) return false;
      try {
        return btoa(atob(m.$bytes)) === m.$bytes;
      } catch {
        return false;
      }
    }
    case 'literal':
      return Object.keys(o).length === 2 && JSON.stringify(o.value) === JSON.stringify(value);
    case 'array': {
      if (Object.keys(o).length !== 2) return false;
      if (!Array.isArray(value)) return false;
      return value.every((item) => validateValueAt(o.item as JSONValue, item, depth + 1, allowLegacyRoot));
    }
    case 'object': {
      if (Object.keys(o).length !== 2) return false;
      if (typeof value !== 'object' || value === null || Array.isArray(value)) return false;
      const shape = o.shape;
      if (typeof shape !== 'object' || shape === null || Array.isArray(shape)) return false;
      return validateDocument(shape as Record<string, unknown>, value as Record<string, unknown>, depth + 1, allowLegacyRoot);
    }
    case 'union': {
      if (Object.keys(o).length !== 2) return false;
      const validators = o.validators;
      if (!Array.isArray(validators)) return false;
      return validators.some((v) => validateValueAt(v, value, depth + 1, allowLegacyRoot));
    }
    case 'id': {
      if (Object.keys(o).length !== 2) return false;
      if (typeof value !== 'string') return false;
      const parsed = parseOpaqueId(value, allowLegacyRoot);
      return typeof o.tableName === 'string' && isIdentifier(o.tableName) && parsed?.table === o.tableName;
    }
    case 'image':
      return validImageDescriptor(o) && typeof value === 'string' && /^pbv_[0-9a-f]{32}$/.test(value);
    case 'record': {
      if (Object.keys(o).length !== 3) return false;
      if (typeof value !== 'object' || value === null || Array.isArray(value)) return false;
      for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
        if (!validateValueAt(o.key as JSONValue, k, depth + 1, allowLegacyRoot) || !validateValueAt(o.value as JSONValue, v, depth + 1, allowLegacyRoot)) return false;
      }
      return true;
    }
    default:
      return false;
  }
}

function validImageDescriptor(o: Record<string, unknown>): boolean {
  if (Object.keys(o).some((key) => !['type', 'thumbs', 'mimeTypes'].includes(key))) return false;
  if (!Array.isArray(o.thumbs) || !Array.isArray(o.mimeTypes) || o.thumbs.length > 16 || o.mimeTypes.length === 0) return false;
  const thumbs = o.thumbs as unknown[];
  const mimeTypes = o.mimeTypes as unknown[];
  if (new Set(thumbs).size !== thumbs.length || new Set(mimeTypes).size !== mimeTypes.length) return false;
  const supported = new Set(['image/gif', 'image/jpeg', 'image/png', 'image/webp']);
  return thumbs.every((value) => {
    if (typeof value !== 'string') return false;
    const match = /^(\d+)x(\d+)(t|b|f)?$/.exec(value);
    if (!match) return false;
    const width = Number(match[1]);
    const height = Number(match[2]);
    return !(match[3] !== undefined && (width === 0 || height === 0)) && !(width === 0 && height === 0) && width <= 4096 && height <= 4096 && (width === 0 || height === 0 || width * height <= 16_777_216);
  }) && mimeTypes.every((value) => typeof value === 'string' && supported.has(value));
}

function validateDocument(shape: Record<string, unknown>, doc: Record<string, unknown>, depth: number, allowLegacyRoot: boolean): boolean {
  if (depth > MAX_VALUE_DEPTH) return false;
  for (const k of Object.keys(doc)) {
    const field = shape[k];
    if (field === undefined) return false;
    if (!validateValueAt(field as JSONValue, doc[k], depth + 1, allowLegacyRoot)) return false;
  }
  for (const k of Object.keys(shape)) {
    const field = shape[k] as Record<string, unknown> | undefined;
    if (!(k in doc) && !isOptionalDescriptor(field as JSONValue)) return false;
  }
  return true;
}

// isOptionalDescriptor reports whether a descriptor is an optional validator.
export function isOptionalDescriptor(value: JSONValue): boolean {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false;
  const o = value as Record<string, unknown>;
  const t = o.type;
  if (t === 'optional' || t === 'defaulted') return true;
  if (t === 'union' && Array.isArray(o.validators)) {
    return o.validators.some((v) => isOptionalDescriptor(v as JSONValue));
  }
  if (t === 'object' && typeof o.shape === 'object' && o.shape !== null && !Array.isArray(o.shape)) {
    return Object.values(o.shape).every((v) => isOptionalDescriptor(v as JSONValue));
  }
  return false;
}
