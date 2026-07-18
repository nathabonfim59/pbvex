import { expectTypeOf } from 'vitest';
import { defineMigration, type MigrationContext } from '../src/runtime/server.js';
import { v } from '../src/runtime/values.js';

const from = v.object({ name: v.string(), nickname: v.optional(v.string()) });
const to = v.object({ name: v.string(), active: v.defaulted(v.boolean(), true) });

defineMigration({
  id: '20260718_add_active',
  table: 'users',
  mode: 'transactional',
  from,
  to,
  up(oldDoc, ctx) {
    expectTypeOf(oldDoc.name).toEqualTypeOf<string>();
    expectTypeOf(oldDoc.nickname).toEqualTypeOf<string | undefined>();
    expectTypeOf(oldDoc._id).toEqualTypeOf<import('../src/runtime/server.js').Id<'users'>>();
    expectTypeOf(oldDoc._creationTime).toEqualTypeOf<number>();
    expectTypeOf(ctx).toEqualTypeOf<MigrationContext>();
    return { name: oldDoc.name, active: true };
  },
  down(newDoc, ctx) {
    expectTypeOf(newDoc.name).toEqualTypeOf<string>();
    expectTypeOf(newDoc.active).toEqualTypeOf<boolean>();
    expectTypeOf(newDoc._id).toEqualTypeOf<import('../src/runtime/server.js').Id<'users'>>();
    expectTypeOf(ctx.fail('stop')).toEqualTypeOf<never>();
    return { name: newDoc.name };
  },
});

defineMigration({
  id: 'objects_only', table: 'users', mode: 'transactional',
  // @ts-expect-error Document migrations require object validators.
  from: v.string(),
  to,
  up: () => ({ name: '', active: true }),
  down: (doc) => ({ name: doc.name }),
});

defineMigration({
  id: 'sync_only', table: 'users', mode: 'transactional', from, to,
  // @ts-expect-error Migration transforms must be synchronous.
  up: async (doc) => ({ name: doc.name, active: true }),
  down: (doc) => ({ name: doc.name }),
});

// @ts-expect-error Reversible migrations require down.
defineMigration({
  id: 'down_required', table: 'users', mode: 'transactional', from, to,
  up: (doc) => ({ name: doc.name, active: true }),
});

defineMigration({
  id: 'no_system_outputs', table: 'users', mode: 'transactional', from, to,
  // @ts-expect-error System fields are not authored migration output.
  up: (doc) => ({ name: doc.name, active: true, _id: 'users:1' }),
  down: (doc) => ({ name: doc.name }),
});
