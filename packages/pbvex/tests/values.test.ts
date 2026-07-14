import { describe, it, expect } from 'vitest';
import { formatOpaqueId } from '@pbvex/protocol';
import { v, ValidationError } from '../src/runtime/values.js';

describe('values validators', () => {
  it('validates string', () => {
    const validator = v.string();
    expect(validator.validate('hello')).toBe('hello');
    expect(() => validator.validate(123 as any)).toThrow(ValidationError);
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
