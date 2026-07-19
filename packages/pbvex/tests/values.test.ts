import { describe, it, expect } from 'vitest';
import { formatOpaqueId } from '@pbvex/protocol';
import { paginationResultValidator } from '../src/runtime/server.js';
import { v, ValidationError } from '../src/runtime/values.js';

describe('values validators', () => {
  it('validates string', () => {
    const validator = v.string();
    expect(validator.validate('hello')).toBe('hello');
    expect(() => validator.validate(123 as any)).toThrow(ValidationError);
  });

  it('validates image storage ids and canonicalizes policy', () => {
    const validator = v.image({ thumbs: ['640x0', '96x96'], mimeTypes: ['image/png', 'image/jpeg'] });
    expect(validator.toJSON()).toEqual({
      type: 'image',
      thumbs: ['640x0', '96x96'],
      mimeTypes: ['image/jpeg', 'image/png'],
    });
    expect(validator.validate('pbv_0123456789abcdef0123456789abcdef')).toBe('pbv_0123456789abcdef0123456789abcdef');
    expect(() => validator.validate('not-storage')).toThrow(ValidationError);
    expect(() => v.image({ thumbs: ['0x0'] })).toThrow(ValidationError);
    expect(() => v.image({ thumbs: ['640x0f'] })).toThrow(ValidationError);
    expect(() => v.image({ thumbs: ['96x96', '96x96'] })).toThrow(ValidationError);
    expect(() => v.image({ mimeTypes: ['image/svg+xml'] })).toThrow(ValidationError);
  });

  it('validates number', () => {
    const validator = v.number();
    expect(validator.validate(42)).toBe(42);
    expect(() => validator.validate('foo')).toThrow(ValidationError);
  });

  it('validates object shape with Convex-shaped args', () => {
    const validator = v.object({ name: v.string(), count: v.number() });
    expect(validator.validate({ name: 'a', count: 1 })).toEqual({ name: 'a', count: 1 });
    expect(() => validator.validate({ name: 'a' })).toThrow(ValidationError);
  });

  it('rejects unknown object fields deterministically instead of stripping them', () => {
    const validator = v.object({ name: v.string() });

    expect(() => validator.validate({ name: 'a', zebra: true, alpha: true } as any)).toThrowError(
      'Unknown field at $["alpha"]',
    );
  });

  it('matches deployed object constraints for shapes and values', () => {
    expect(() => v.object({ $invalid: v.string() } as any)).toThrow('Invalid object field name');
    expect(() => v.object({ value: {} } as any)).toThrow('Invalid validator for object field');
    expect(() => v.object(Object.assign(Object.create({}), { value: v.string() }) as any)).toThrow(
      'Object validator shape must be a plain object',
    );

    const validator = v.object({ value: v.string() });
    expect(() => validator.validate(Object.assign(new (class Value {})(), { value: 'ok' }))).toThrow('Expected object');
    expect(validator.validate(Object.assign(Object.create(null), { value: 'ok' }))).toEqual({ value: 'ok' });
  });

  it('reports paths for unknown fields in nested constrained objects', () => {
    const validator = v.object({
      profile: v.object({ name: v.string() }),
      entries: v.array(v.object({ value: v.number() })),
    });

    expect(() => validator.validate({ profile: { name: 'a', extra: true }, entries: [] } as any)).toThrowError(
      'Unknown field at $["profile"]["extra"]',
    );
    expect(() => validator.validate({ profile: { name: 'a' }, entries: [{ value: 1, extra: true }] } as any)).toThrowError(
      'Unknown field at $["entries"][0]["extra"]',
    );
  });

  it('validates optional fields', () => {
    const validator = v.object({ name: v.string(), channel: v.optional(v.string()) });
    expect(validator.validate({ name: 'a' })).toEqual({ name: 'a', channel: undefined });
    expect(validator.validate({ name: 'a', channel: 'x' })).toEqual({ name: 'a', channel: 'x' });
  });

  it('validates defaulted fields', () => {
    const validator = v.object({ name: v.string(), channel: v.defaulted(v.string(), 'general') });
    expect(validator.validate({ name: 'a' })).toEqual({ name: 'a', channel: 'general' });
    expect(validator.validate({ name: 'a', channel: 'x' })).toEqual({ name: 'a', channel: 'x' });
  });

  it('preserves optional/defaulted behavior while rejecting extras', () => {
    const validator = v.object({
      note: v.optional(v.string()),
      channel: v.defaulted(v.string(), 'general'),
    });

    expect(validator.validate({})).toEqual({ note: undefined, channel: 'general' });
    expect(() => validator.validate({ extra: true } as any)).toThrowError('Unknown field at $["extra"]');
  });

  it('composes strict object validators with deterministic replacement and field order', () => {
    const base = v.object({ id: v.string(), count: v.number(), note: v.optional(v.string()) });
    const extended = base.extend({ count: v.string(), active: v.boolean() });
    const picked = extended.pick('active', 'id');
    const omitted = extended.omit('count', 'note');

    expect(extended.validate({ id: 'a', count: 'two', active: true })).toEqual({
      id: 'a', count: 'two', note: undefined, active: true,
    });
    expect(() => extended.validate({ id: 'a', count: 2, active: true } as any)).toThrow('Expected string');
    expect(() => picked.validate({ active: true, id: 'a', extra: true } as any)).toThrow('Unknown field');
    expect(Object.keys((extended.toJSON() as any).shape)).toEqual(['id', 'count', 'note', 'active']);
    expect(Object.keys((picked.toJSON() as any).shape)).toEqual(['active', 'id']);
    expect(Object.keys((omitted.toJSON() as any).shape)).toEqual(['id', 'active']);
    expect((extended.toJSON() as any).shape.count).toEqual({ type: 'string' });
  });

  it('makes object fields optional without double-wrapping existing optionals', () => {
    const partial = v.object({
      name: v.string(),
      note: v.optional(v.string()),
      retries: v.defaulted(v.number(), 3),
    }).partial();

    expect(partial.validate({})).toEqual({ name: undefined, note: undefined, retries: undefined });
    expect(partial.validate({ retries: 5 })).toEqual({ name: undefined, note: undefined, retries: 5 });
    expect(partial.toJSON()).toEqual({
      type: 'object',
      shape: {
        name: { type: 'optional', validator: { type: 'string' } },
        note: { type: 'optional', validator: { type: 'string' } },
        retries: {
          type: 'optional',
          validator: { type: 'defaulted', validator: { type: 'number' }, defaultValue: 3 },
        },
      },
    });
  });

  it('does not mutate object validators or their source shapes during composition', () => {
    const source = { name: v.string(), count: v.number() };
    const base = v.object(source);
    const before = base.toJSON();
    const composed = base.extend({ count: v.string() }).omit('name');

    source.name = v.number() as any;
    source.count = v.string() as any;

    expect(base.toJSON()).toEqual(before);
    expect(base.validate({ name: 'a', count: 1 })).toEqual({ name: 'a', count: 1 });
    expect(composed.validate({ count: 'one' })).toEqual({ count: 'one' });
    expect(composed).not.toBe(base);
  });

  it('normalizes and wire-encodes defaults eagerly', () => {
    const validator = v.defaulted(v.object({ count: v.number(), note: v.optional(v.string()) }), {
      count: 2,
    });
    expect(validator.validate(undefined)).toEqual({ count: 2, note: undefined });
    expect(validator.toJSON()).toEqual({
      type: 'defaulted',
      validator: {
        type: 'object',
        shape: {
          count: { type: 'number' },
          note: { type: 'optional', validator: { type: 'string' } },
        },
      },
      defaultValue: { count: 2 },
    });
    expect(() => v.defaulted(v.number(), 'bad' as never)).toThrow('Expected finite number');
  });

  it('emits the record key descriptor', () => {
    expect(v.record(v.string(), v.number()).toJSON()).toEqual({
      type: 'record',
      key: { type: 'string' },
      value: { type: 'number' },
    });
  });

  it('keeps records open-ended for valid dynamic keys', () => {
    const validator = v.record(v.string(), v.number());

    expect(validator.validate({ beta: 2, alpha: 1 })).toEqual({ beta: 2, alpha: 1 });
  });

  it('rejects serialization of delayed validators', () => {
    const validator = v.delayed(() => v.string());
    expect(validator.validate('ok')).toBe('ok');
    expect(() => validator.toJSON()).toThrow('Delayed validators cannot be serialized');
  });

  it('validates union literals', () => {
    const validator = v.union(v.literal('admin'), v.literal('user'));
    expect(validator.validate('admin')).toBe('admin');
    expect(() => validator.validate('guest')).toThrow(ValidationError);
  });

  it('validates array of ids', () => {
    const validator = v.array(v.id('messages'));
		const mac = new Uint8Array(32);
    const validA = formatOpaqueId({ version: 2, keyId: 1n, namespace: 'root', table: 'messages', raw: 'a'.repeat(15) }, mac);
    const validB = formatOpaqueId({ version: 2, keyId: 1n, namespace: 'root', table: 'messages', raw: 'b'.repeat(15) }, mac);
    expect(validator.validate([validA, validB])).toEqual([validA, validB]);
    expect(() => validator.validate([validA, 'not-an-id'])).toThrow(ValidationError);
		expect(() => validator.validate([validA, formatOpaqueId({ version: 2, keyId: 1n, namespace: 'root', table: 'other', raw: 'b'.repeat(15) }, mac)])).toThrow(ValidationError);
  });

  it('validates canonical closed pagination results and emits their descriptor', () => {
    const validator = paginationResultValidator(v.object({ name: v.string() }));
    const result = { page: [{ name: 'first' }], isDone: true, continueCursor: '' };

    expect(validator.validate(result)).toEqual(result);
    expect(validator.toJSON()).toEqual({
      type: 'object',
      shape: {
        page: { type: 'array', item: { type: 'object', shape: { name: { type: 'string' } } } },
        isDone: { type: 'boolean' },
        continueCursor: { type: 'string' },
      },
    });
    expect(() => validator.validate({ ...result, extra: true } as any)).toThrowError('Unknown field at $["extra"]');
    expect(() => validator.validate({ ...result, page: [{ name: 1 }] } as any)).toThrow(ValidationError);
    expect(() => validator.validate({ ...result, isDone: 'yes' } as any)).toThrow(ValidationError);
    expect(() => validator.validate({ ...result, continueCursor: null } as any)).toThrow(ValidationError);

    // Cursor/completion consistency is runtime behavior, not a cross-field validator constraint.
    expect(validator.validate({ ...result, isDone: false })).toEqual({ ...result, isDone: false });
  });

  it('infers type for literal', () => {
    const validator = v.literal('ok');
    const value: 'ok' = validator.validate('ok');
    expect(value).toBe('ok');
  });

  it('serializes validators to JSON', () => {
    const validator = v.object({ name: v.string(), tags: v.array(v.string()) });
    const json = validator.toJSON();
    expect(json).toEqual({
      type: 'object',
      shape: {
        name: { type: 'string' },
        tags: { type: 'array', item: { type: 'string' } },
      },
    });
  });

  it('emits the deployable record/defaulted descriptor contract', () => {
    const validator = v.record(v.union(v.string(), v.literal('fixed')), v.defaulted(v.number(), 2));
    expect(validator.validate({ fixed: undefined as any, other: 4 })).toEqual({ fixed: 2, other: 4 });
    expect(validator.toJSON()).toEqual({
      type: 'record',
      key: { type: 'union', validators: [{ type: 'string' }, { type: 'literal', value: 'fixed' }] },
      value: { type: 'defaulted', validator: { type: 'number' }, defaultValue: 2 },
    });
  });

  it('does not emit undeployable delayed or invalid record-key descriptors', () => {
    expect(() => v.delayed(() => v.string()).toJSON()).toThrow(ValidationError);
    expect(() => v.record(v.number() as any, v.string()).toJSON()).toThrow(ValidationError);
    expect(() => v.number().validate(Infinity)).toThrow(ValidationError);
  });

  it('emits a bounded, executable recursive descriptor with named refs', () => {
    let tree: any;
    tree = v.recursive('Node', () =>
      v.object({ name: v.string(), children: v.array(tree) }),
    );
    // Serialization terminates and produces the deployable recursive contract:
    // the wrapper declares the name, and the cycle point emits a ref.
    expect(() => tree.toJSON()).not.toThrow();
    const json = tree.toJSON() as any;
    expect(json).toEqual({
      type: 'recursive',
      name: 'Node',
      validator: {
        type: 'object',
        shape: {
          name: { type: 'string' },
          children: { type: 'array', item: { type: 'ref', name: 'Node' } },
        },
      },
    });
    // Repeated serialization is deterministic (no leaked emit flag).
    expect(tree.toJSON()).toEqual(json);
    // JSON-deployable end to end.
    expect(() => JSON.stringify(json)).not.toThrow();
  });

  it('validates nested data through a recursive validator', () => {
    let tree: any;
    tree = v.recursive('Node', () =>
      v.object({ name: v.string(), children: v.array(tree) }),
    );
    const result = tree.validate({ name: 'root', children: [{ name: 'a', children: [] }] });
    expect(result).toEqual({ name: 'root', children: [{ name: 'a', children: [] }] });
    // Missing required nested field is rejected.
    expect(() => tree.validate({ name: 'root', children: [{ children: [] }] })).toThrow(ValidationError);
    // Wrong nested type is rejected.
    expect(() => tree.validate({ name: 'root', children: [{ name: 1, children: [] }] })).toThrow(ValidationError);
  });

  it('rejects malformed recursive names', () => {
    expect(() => v.recursive('1bad', () => v.string())).toThrow(ValidationError);
  });
});
