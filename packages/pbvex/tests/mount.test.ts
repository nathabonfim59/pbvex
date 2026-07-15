import { describe, it, expect } from 'vitest';
import { defineComponent, mount, defineApp, defineComponentFns } from '../src/runtime/server.js';
import { v } from '../src/runtime/values.js';

describe('mount runtime', () => {
  it('does not throw for a component without args', () => {
    const comp = defineComponent({ modulePaths: ['store.ts'] });
    expect(() => mount(comp, 'test')).not.toThrow();
    const m = mount(comp, 'test');
    expect(m.name).toBe('test');
    expect(m.component).toBeDefined();
  });

  it('does not throw for a component with args', () => {
    const comp = defineComponent({
      modulePaths: ['store.ts'],
      args: v.object({ label: v.string() }),
    });
    expect(() => mount(comp, 'test', { args: { label: 'hi' } })).not.toThrow();
    const m = mount(comp, 'test', { args: { label: 'hi' } });
    expect(m.name).toBe('test');
  });

  it('does not throw for fully-defaulted args', () => {
    const comp = defineComponent({
      modulePaths: ['store.ts'],
      args: v.object({ retries: v.defaulted(v.number(), 3) }),
    });
    expect(() => mount(comp, 'test')).not.toThrow();
    const m = mount(comp, 'test');
    expect(m.name).toBe('test');
  });

  it('defineApp accepts mount results', () => {
    const comp = defineComponent({ modulePaths: ['store.ts'] });
    const app = defineApp({ components: [mount(comp, 'a')] });
    expect(app.components).toHaveLength(1);
  });

  it('canonicalizes object omissions and rejects undefined array elements', () => {
    const objectComp = defineComponent({
      modulePaths: ['object.ts'],
      args: v.object({ value: v.optional(v.string()) }),
    });
    expect(mount(objectComp, 'object', { args: { value: undefined } }).args).toEqual({});

    const arrayComp = defineComponent({
      modulePaths: ['array.ts'],
      args: v.array(v.defaulted(v.number(), 1)),
    });
    expect(() => mount(arrayComp, 'array', { args: [undefined] } as never)).toThrow(
      'undefined is not a valid PBVex value at root[0]',
    );
  });

  it('defineComponentFns produces callable query factory', () => {
    const comp = defineComponent({
      modulePaths: ['store.ts'],
      args: v.object({ label: v.string() }),
    });
    const fns = defineComponentFns(comp);
    const fn = fns.query({ handler: async (ctx) => ctx.args.label });
    expect(fn.type).toBe('query');
  });
});
