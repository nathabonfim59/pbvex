import { decodeId, encodeValue, isIdentifier, isSafeFieldName } from '@pbvex/protocol';
import type { JSONValue, PbvexValue } from '@pbvex/protocol';
import type { StorageId } from '@pbvex/protocol';

export type GenericId<TableName extends string = string> = string & { __table: TableName };

const VALIDATOR_KINDS = new Set<ValidatorKind>([
  'id', 'image', 'string', 'number', 'int64', 'float64', 'boolean', 'bytes', 'any', 'null', 'literal',
  'object', 'array', 'record', 'union', 'optional', 'defaulted', 'delayed', 'recursive',
]);

export type Validator<Out = any, In = Out> = {
  readonly __type: Out;
  readonly __inputType?: In;
  readonly kind: ValidatorKind;
  readonly optional: boolean;
  readonly isValidatorBrand: true;
  validate(value: unknown): Out;
  toJSON(): unknown;
};

export type ObjectValidator<Out extends Record<string, any> = Record<string, any>, In extends Record<string, any> = Out> =
  Validator<Out, In> & { readonly kind: 'object' };

export type ValidatorKind =
  | 'id'
  | 'image'
  | 'string'
  | 'number'
  | 'int64'
  | 'float64'
  | 'boolean'
  | 'bytes'
  | 'any'
  | 'null'
  | 'literal'
  | 'object'
  | 'array'
  | 'record'
  | 'union'
  | 'optional'
  | 'defaulted'
  | 'delayed'
  | 'recursive';

interface BaseValidator<T> extends Validator<T, T> {
  optional: false;
}

class V<Out, In = Out> implements Validator<Out, In> {
  readonly __type!: Out;
  readonly __inputType?: In;
  readonly isValidatorBrand: true = true;
  constructor(
    readonly kind: ValidatorKind,
    readonly validate: (value: unknown) => Out,
    readonly optional: boolean,
    private readonly json: () => unknown,
  ) {}
  toJSON(): unknown {
    return this.json();
  }
}

export function id<TableName extends string = string>(tableName: TableName): Validator<GenericId<TableName>> {
  if (!isIdentifier(tableName)) {
    throw new ValidationError(`Invalid table identifier for id: ${JSON.stringify(tableName)}`);
  }
  return new V<GenericId<TableName>>(
    'id',
    (value) => {
      if (typeof value !== 'string') throw new ValidationError('Expected string id');
	  const decoded = decodeId(value);
	  if (!decoded.ok || decoded.table !== tableName) throw new ValidationError(`Expected id for table ${tableName}`);
      return value as GenericId<TableName>;
    },
    false,
    () => ({ type: 'id', tableName }),
  );
}

export interface ImageValidatorOptions {
  readonly thumbs?: readonly string[];
  readonly mimeTypes?: readonly string[];
}

const imageThumbPattern = /^(\d+)x(\d+)(t|b|f)?$/;
const defaultImageMimeTypes = ['image/gif', 'image/jpeg', 'image/png', 'image/webp'] as const;

export function image(options: ImageValidatorOptions = {}): Validator<StorageId> {
  const thumbs = [...(options.thumbs ?? [])].sort();
  const mimeTypes = [...(options.mimeTypes ?? defaultImageMimeTypes)].sort();
  if (thumbs.length > 16 || new Set(thumbs).size !== thumbs.length) {
    throw new ValidationError('Image thumbs must be unique and contain at most 16 entries');
  }
  for (const thumb of thumbs) {
    const match = imageThumbPattern.exec(thumb);
    const width = match ? Number(match[1]) : 0;
    const height = match ? Number(match[2]) : 0;
    if (!match || (match[3] !== undefined && (width === 0 || height === 0)) || (width === 0 && height === 0) || width > 4096 || height > 4096 || (width > 0 && height > 0 && width * height > 16_777_216)) {
      throw new ValidationError(`Invalid image thumb: ${JSON.stringify(thumb)}`);
    }
  }
  if (mimeTypes.length === 0 || new Set(mimeTypes).size !== mimeTypes.length || mimeTypes.some((type) => !defaultImageMimeTypes.includes(type as any))) {
    throw new ValidationError('Image mimeTypes must be a non-empty unique list of supported image MIME types');
  }
  const descriptor = Object.freeze({ type: 'image', thumbs: Object.freeze(thumbs), mimeTypes: Object.freeze(mimeTypes) });
  return new V<StorageId>(
    'image',
    (value) => {
      if (typeof value !== 'string' || !/^pbv_[0-9a-f]{32}$/.test(value)) throw new ValidationError('Expected storage image id');
      return value as StorageId;
    },
    false,
    () => descriptor,
  );
}

export function string(): Validator<string> {
  return new V<string>(
    'string',
    (value) => {
      if (typeof value !== 'string') throw new ValidationError('Expected string');
      return value;
    },
    false,
    () => ({ type: 'string' }),
  );
}

export function number(): Validator<number> {
  return new V<number>(
    'number',
    (value) => {
      if (typeof value !== 'number' || !Number.isFinite(value)) throw new ValidationError('Expected finite number');
      return value;
    },
    false,
    () => ({ type: 'number' }),
  );
}

const MIN_INT64 = BigInt('-9223372036854775808');
const MAX_INT64 = BigInt('9223372036854775807');

export function int64(): Validator<bigint> {
  return new V<bigint>(
    'int64',
    (value) => {
      if (typeof value !== 'bigint') throw new ValidationError('Expected int64');
      if (value < MIN_INT64 || value > MAX_INT64) throw new ValidationError('int64 out of range');
      return value;
    },
    false,
    () => ({ type: 'int64' }),
  );
}

export function bigint(): Validator<bigint> {
  return int64();
}

export function float64(): Validator<number> {
  return new V<number>(
    'float64',
    (value) => {
      if (typeof value !== 'number' || !Number.isFinite(value)) throw new ValidationError('Expected finite number');
      return value;
    },
    false,
    () => ({ type: 'float64' }),
  );
}

export function bytes(): Validator<ArrayBuffer> {
  return new V<ArrayBuffer>(
    'bytes',
    (value) => {
      if (!(value instanceof ArrayBuffer)) throw new ValidationError('Expected bytes');
      return value;
    },
    false,
    () => ({ type: 'bytes' }),
  );
}

export function boolean(): Validator<boolean> {
  return new V<boolean>(
    'boolean',
    (value) => {
      if (typeof value !== 'boolean') throw new ValidationError('Expected boolean');
      return value;
    },
    false,
    () => ({ type: 'boolean' }),
  );
}

export function any(): Validator<any> {
  return new V<any>('any', (value) => value, false, () => ({ type: 'any' }));
}

export function nullType(): Validator<null> {
  return new V<null>(
    'null',
    (value) => {
      if (value !== null) throw new ValidationError('Expected null');
      return value;
    },
    false,
    () => ({ type: 'null' }),
  );
}

export function literal<T extends string | number | bigint | boolean>(value: T): Validator<T> {
  if (typeof value === 'number' && !Number.isFinite(value)) {
    throw new ValidationError('Literal value must be a finite number');
  }
  let wireValue: JSONValue;
  try {
    wireValue = encodeValue(value as PbvexValue) as JSONValue;
  } catch (err) {
    throw new ValidationError(err instanceof Error ? err.message : String(err));
  }
  return new V<T>(
    'literal',
    (v) => {
      if (v !== value) throw new ValidationError(`Expected literal ${String(value)}`);
      return value;
    },
    false,
    () => ({ type: 'literal', value: wireValue }),
  );
}

type OutputOf<V> = V extends Validator<infer Out, any> ? Out : never;
type InputOf<V> = V extends Validator<any, infer In> ? In : never;
type ObjectOutput<T extends Record<string, Validator<any, any>>> = { [K in keyof T]: OutputOf<T[K]> };
type ObjectInput<T extends Record<string, Validator<any, any>>> = {
  [K in keyof T as undefined extends InputOf<T[K]> ? never : K]: InputOf<T[K]>;
} & {
  [K in keyof T as undefined extends InputOf<T[K]> ? K : never]?: Exclude<InputOf<T[K]>, undefined>;
};

export function object<T extends Record<string, Validator<any, any>>>(
  shape: T,
): ObjectValidator<ObjectOutput<T>, ObjectInput<T>> {
  return new V(
    'object',
    (value) => {
      if (typeof value !== 'object' || value === null || Array.isArray(value)) {
        throw new ValidationError('Expected object');
      }
      const result: any = {};
      for (const key of Object.keys(shape)) {
        result[key] = shape[key].validate((value as any)[key]);
      }
      return result;
    },
    false,
    () => ({ type: 'object', shape: Object.fromEntries(Object.entries(shape).map(([k, v]) => [k, v.toJSON()])) }),
  ) as ObjectValidator<ObjectOutput<T>, ObjectInput<T>>;
}

export function array<T extends Validator<any, any>>(item: T): Validator<OutputOf<T>[], Array<Exclude<InputOf<T>, undefined>>> {
  return new V<OutputOf<T>[], Array<Exclude<InputOf<T>, undefined>>>(
    'array',
    (value) => {
      if (!Array.isArray(value)) throw new ValidationError('Expected array');
      return value.map((v) => item.validate(v));
    },
    false,
    () => ({ type: 'array', item: item.toJSON() }),
  );
}

export function record<KOut extends string, KIn extends string, Value extends Validator<any, any>>(
	key: Validator<KOut, KIn>,
	value: Value,
): Validator<Record<KOut, OutputOf<Value>>, Record<KIn, Exclude<InputOf<Value>, undefined>>> {
  return new V<Record<KOut, OutputOf<Value>>, Record<KIn, Exclude<InputOf<Value>, undefined>>>(
    'record',
    (v) => {
      if (typeof v !== 'object' || v === null || Array.isArray(v)) throw new ValidationError('Expected record');
      const result: any = {};
		for (const rawKey of Object.keys(v)) {
			const normalizedKey = key.validate(rawKey);
			if (typeof normalizedKey !== 'string') throw new ValidationError('Record keys must normalize to strings');
			result[normalizedKey] = value.validate((v as any)[rawKey]);
      }
      return result;
    },
    false,
		() => {
			const keyJSON = key.toJSON();
			if (!isRecordKeyDescriptor(keyJSON)) throw new ValidationError('Record keys must use string, string literal, or a union of those validators');
			return { type: 'record', key: keyJSON, value: value.toJSON() };
		},
  );
}

export function union<T extends readonly Validator<any, any>[]>(
  ...validators: T
): Validator<OutputOf<T[number]>, InputOf<T[number]>> {
  return new V<OutputOf<T[number]>, InputOf<T[number]>>(
    'union',
    (value) => {
      const errors: string[] = [];
      for (const validator of validators) {
        try {
          return validator.validate(value);
        } catch (err) {
          errors.push(err instanceof ValidationError ? err.message : String(err));
        }
      }
      throw new ValidationError(`Union validation failed: ${errors.join('; ')}`);
    },
    false,
    () => ({ type: 'union', validators: validators.map((v) => v.toJSON()) }),
  );
}

export function optional<T extends Validator<any, any>>(validator: T): Validator<OutputOf<T> | undefined, InputOf<T> | undefined> {
  const validate = (value: unknown): OutputOf<T> | undefined => {
    if (value === undefined) return undefined;
    return validator.validate(value);
  };
  return new V<OutputOf<T> | undefined, InputOf<T> | undefined>('optional', validate, true, () => ({ type: 'optional', validator: validator.toJSON() }));
}

export function defaulted<T extends Validator<any, any>>(validator: T, defaultValue: OutputOf<T>): Validator<OutputOf<T>, InputOf<T> | undefined> {
	// The backend persists the normalized child value, not the raw argument.
	// Eagerly normalize here so authoring artifacts and deploy-time validation
	// share the exact defaulted contract. Wire-encode the default so bigint and
	// bytes defaults remain JSON-deployable.
	const normalizedDefault = validator.validate(defaultValue);
	const encodedDefault = encodeValue(normalizedDefault as PbvexValue) as JSONValue;
  return new V<OutputOf<T>, InputOf<T> | undefined>(
    'defaulted',
    (value) => {
		if (value === undefined) return normalizedDefault;
      return validator.validate(value);
    },
    false,
		() => ({ type: 'defaulted', validator: validator.toJSON(), defaultValue: encodedDefault }),
  );
}

export function delayed<T extends Validator<any, any>>(factory: () => T): Validator<OutputOf<T>, InputOf<T>> {
  return new V<OutputOf<T>, InputOf<T>>(
    'delayed',
    (value) => factory().validate(value),
    false,
		// Delayed/recursive validators are useful while authoring TypeScript
		// values, but a closure or cyclic graph has no executable protocol v1
		// descriptor. Do not emit a descriptor Go will reject later.
		() => {
			throw new ValidationError('Delayed validators cannot be serialized in deployment manifests');
		},
  );
}

/**
 * Declares a named, serializable recursive validator. The descriptor is
 * `{type:'recursive', name, validator}` where `validator` is the full inner
 * descriptor; cycle points inside it emit `{type:'ref', name}`. The backend
 * resolves refs against the enclosing recursive declaration, so recursive
 * types are genuinely executable (document insert/patch validation, not just
 * manifest acceptance).
 */
export function recursive<T>(name: string, factory: () => Validator<T>): Validator<T> {
  if (!isIdentifier(name)) {
    throw new ValidationError(`Invalid recursive name: ${JSON.stringify(name)}`);
  }
  let emitting = false;
  return new V<T>(
    'recursive',
    (value) => factory().validate(value),
    false,
    () => {
      if (emitting) {
        return { type: 'ref', name };
      }
      emitting = true;
      try {
        return { type: 'recursive', name, validator: factory().toJSON() };
      } finally {
        emitting = false;
      }
    },
  );
}

function isRecordKeyDescriptor(value: unknown): boolean {
	if (typeof value !== 'object' || value === null || Array.isArray(value)) return false;
	const descriptor = value as Record<string, unknown>;
	if (descriptor.type === 'string') return Object.keys(descriptor).length === 1;
	if (descriptor.type === 'literal') return Object.keys(descriptor).length === 2 && typeof descriptor.value === 'string';
	if (descriptor.type !== 'union' || Object.keys(descriptor).length !== 2 || !Array.isArray(descriptor.validators) || descriptor.validators.length === 0 || descriptor.validators.length > 64) return false;
	return descriptor.validators.every(isRecordKeyDescriptor);
}

export class ValidationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ValidationError';
  }
}

export function isValidator<T>(value: unknown): value is Validator<T> {
  if (value === null || typeof value !== 'object') return false;
  const obj = value as Record<string, unknown>;
  return (
    typeof obj.validate === 'function' &&
    typeof obj.toJSON === 'function' &&
    typeof obj.kind === 'string' &&
    VALIDATOR_KINDS.has(obj.kind as ValidatorKind) &&
    typeof obj.optional === 'boolean' &&
    obj.isValidatorBrand === true
  );
}

export const v = {
  id,
  image,
  string,
  number,
  int64,
  bigint,
  float64,
  boolean,
  bytes,
  any,
  null: nullType,
  literal,
  object,
  array,
  record,
  union,
  optional,
  defaulted,
  delayed,
  recursive,
};
