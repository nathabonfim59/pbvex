// Compile-time fixture: verifies typed Args/Env through defineComponent,
// mount(), and component-scoped function contexts. Positive cases typecheck;
// negative cases use ts-expect-error to prove rejections.

import { defineComponent, defineComponentFns, mount, defineApp } from 'pbvex/server';
import { v } from 'pbvex/values';

// ---------------------------------------------------------------------------
// Component with required + optional + defaulted object args and typed env
// ---------------------------------------------------------------------------

const counter = defineComponent({
  modulePaths: ['store.ts'],
  args: v.object({
    label: v.string(),
    count: v.optional(v.number()),
    retries: v.defaulted(v.number(), 3),
  }),
  env: {
    GREETING: { type: 'value', value: 'hi' },
    TOKEN: { type: 'envVar', name: 'SECRET_TOKEN' },
  },
});

const fns = defineComponentFns(counter);

// Positive: ctx.args — retries is number (not undefined) after default resolution
const getLabel = fns.query({
  returns: v.string(),
  handler: async (ctx) => {
    const label: string = ctx.args.label;
    const count: number | undefined = ctx.args.count;
    const retries: number = ctx.args.retries;
    const greeting: string = ctx.env.GREETING;
    const token: string = ctx.env.TOKEN;
    return `${label}:${count}:${retries}:${greeting}:${token}`;
  },
});

// Positive: mount with required args; optional and defaulted omitted
defineApp({ components: [mount(counter, 'counter', { args: { label: 'hello' } })] });

// Positive: mount with all args
defineApp({ components: [mount(counter, 'counter2', { args: { label: 'hello', count: 42, retries: 5 } })] });

// @ts-expect-error required arg omitted — label is required
mount(counter, 'bad1');

// @ts-expect-error wrong arg type
mount(counter, 'bad2', { args: { label: 123 } });

// @ts-expect-error nonexistent env key
const _badEnv = fns.query({ handler: async (ctx) => ctx.env.NONEXISTENT });

// ---------------------------------------------------------------------------
// Top-level v.optional — In is string | undefined
// ---------------------------------------------------------------------------

const optComp = defineComponent({ modulePaths: ['opt.ts'], args: v.optional(v.string()) });
const optFns = defineComponentFns(optComp);

// Positive: options omitted (In = string | undefined)
defineApp({ components: [mount(optComp, 'opt1')] });

// Positive: explicit value
defineApp({ components: [mount(optComp, 'opt2', { args: 'hello' })] });

// ctx.args is string | undefined
const getOpt = optFns.query({ returns: v.string(), handler: async (ctx) => ctx.args ?? 'd' });

// @ts-expect-error wrong type for top-level optional
mount(optComp, 'bad3', { args: 123 });

// ---------------------------------------------------------------------------
// Top-level v.defaulted — In is string | undefined
// ---------------------------------------------------------------------------

const defComp = defineComponent({ modulePaths: ['def.ts'], args: v.defaulted(v.string(), 'fb') });
const defFns = defineComponentFns(defComp);

// Positive: options omitted
defineApp({ components: [mount(defComp, 'def1')] });

// Positive: explicit value
defineApp({ components: [mount(defComp, 'def2', { args: 'explicit' })] });

// ctx.args is string (default resolved)
const getDef = defFns.query({ returns: v.string(), handler: async (ctx) => ctx.args });

// @ts-expect-error wrong type for top-level defaulted
mount(defComp, 'bad4', { args: 123 });

// ---------------------------------------------------------------------------
// All-defaulted object args — options can be omitted
// ---------------------------------------------------------------------------

const settings = defineComponent({
  modulePaths: ['settings.ts'],
  args: v.object({ retries: v.defaulted(v.number(), 3) }),
});

// Positive: options omitted (all fields are optional in input type)
defineApp({ components: [mount(settings, 'settings')] });

// Positive: explicit value
defineApp({ components: [mount(settings, 'settings2', { args: { retries: 5 } })] });

// ctx.args.retries is number
const settingsFns = defineComponentFns(settings);
const getRetries = settingsFns.query({ returns: v.number(), handler: async (ctx) => ctx.args.retries });

// Positive: input/output propagation survives serializable nested combinators.
const nested = defineComponent({
  modulePaths: ['nested.ts'],
  args: v.object({
    rows: v.array(v.object({ count: v.defaulted(v.number(), 1) })),
    labels: v.record(v.string(), v.defaulted(v.string(), 'default')),
  }),
});
mount(nested, 'nested', { args: { rows: [{}], labels: {} } });
const nestedFns = defineComponentFns(nested);
nestedFns.query({ handler: async (ctx) => ctx.args.rows[0].count });

// Container positions cannot represent undefined in deployment JSON.
const arrayDefaults = defineComponent({ modulePaths: ['array-defaults.ts'], args: v.array(v.defaulted(v.number(), 1)) });
// @ts-expect-error undefined array elements would serialize as null
mount(arrayDefaults, 'badArrayDefaults', { args: [undefined] });
const recordDefaults = defineComponent({ modulePaths: ['record-defaults.ts'], args: v.record(v.string(), v.defaulted(v.number(), 1)) });
// @ts-expect-error undefined record values would be dropped during JSON serialization
mount(recordDefaults, 'badRecordDefaults', { args: { missing: undefined } });

// Delayed closures are useful for local validation but cannot be represented
// in protocol v1 deployment manifests.
const delayed = v.delayed(() => v.string());
const delayedValue: string = delayed.validate('ok');

// v.any() is an actual args validator, so its args remain required despite
// TypeScript's special any assignability rules.
const anyArgs = defineComponent({ modulePaths: ['any.ts'], args: v.any() });
// @ts-expect-error v.any args are required
mount(anyArgs, 'missingAny');
mount(anyArgs, 'presentAny', { args: null });

// ---------------------------------------------------------------------------
// Required-field object — options mandatory
// ---------------------------------------------------------------------------

const required = defineComponent({
  modulePaths: ['req.ts'],
  args: v.object({ name: v.string() }),
});

// @ts-expect-error required field omitted
mount(required, 'bad5');

// Positive: required field provided
defineApp({ components: [mount(required, 'req1', { args: { name: 'hi' } })] });

// ---------------------------------------------------------------------------
// Component with no args
// ---------------------------------------------------------------------------

const plain = defineComponent({ modulePaths: ['plain.ts'] });

// Positive: options omitted
defineApp({ components: [mount(plain, 'plain')] });

// @ts-expect-error args rejected for no-args component at compile time
mount(plain, 'badNoArgs', { args: 'value' });

// ---------------------------------------------------------------------------
// Component with envVar-only env
// ---------------------------------------------------------------------------

const secure = defineComponent({
  modulePaths: ['secure.ts'],
  env: { API_KEY: { type: 'envVar', name: 'PBVEX_API_KEY' } },
});

const secureFns = defineComponentFns(secure);

const checkKey = secureFns.query({
  returns: v.boolean(),
  handler: async (ctx) => ctx.env.API_KEY.length > 0,
});

// @ts-expect-error wrong env key
const _badSecureEnv = secureFns.query({ handler: async (ctx) => ctx.env.DATABASE_URL });

// ---------------------------------------------------------------------------
// defineApp typed boundary — direct mount rejected (missing opaque brand)
// ---------------------------------------------------------------------------

// @ts-expect-error direct mount object cannot forge the opaque brand
defineApp({ components: [{ component: counter, name: 'bypass', args: { label: 'x' } }] });

export { counter, optComp, defComp, settings, plain, secure, required, getLabel, getOpt, getDef, checkKey, getRetries };
