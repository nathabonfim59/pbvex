import { v } from 'pbvex/values';
import { query, mutation, action } from './_generated/server';
import type { QueryCtx, MutationCtx, ActionCtx, Doc, Id, JobId } from './_generated/server';
import { api, internal } from './_generated/api';

export const _userQuery = query({
  args: { id: v.id('users') },
  returns: v.id('users'),
  handler: async (ctx, args) => {
    const user = await ctx.db.get(args.id);
    if (user) return user._id;
    const normalized: Id<'users'> | null = ctx.db.normalizeId('users', 'placeholder');
    return normalized ?? args.id;
  },
});

export const _listQuery = query({
  args: { channel: v.string() },
  returns: v.array(v.string()),
  handler: async (ctx, args) => {
    return [];
  },
});

export const _noArgsQuery = query({
  returns: v.number(),
  handler: async (ctx, args) => {
    const noop: {} = args;
    return 0;
  },
});

export const _sendMutation = mutation({
  args: { channel: v.string(), body: v.string() },
  returns: v.id('messages'),
  handler: async (ctx, args) => {
    const userId = ctx.db.normalizeId('users', 'uid');
    if (userId === null) {
      throw new Error('user not found');
    }
    const messageId = await ctx.db.insert('messages', { body: args.body, author: userId, channel: args.channel });
    await ctx.db.patch(messageId, { channel: 'updated' });
    await ctx.db.replace(messageId, { body: args.body, author: userId, channel: args.channel });
    await ctx.db.delete(messageId);
    return messageId;
  },
});

export const _action = action({
  handler: async (ctx, args) => {
    const listResult: Promise<string[]> = ctx.run(api.messages.list, { channel: 'x' });
    const sendResult: Promise<Id<'messages'>> = ctx.run(api.messages.send, { channel: 'x', body: 'x' });
    const statsResult: Promise<number> = ctx.run(internal.messages.internalStats, {});
  },
});

export const _userId: Id<'users'> = v.id('users').validate('uid');
// @ts-expect-error
export const _badId: Id<'users'> = 'uid';
// @ts-expect-error
export const _crossTableId: Id<'messages'> = _userId;

declare const queryCtx: QueryCtx;
declare const mutationCtx: MutationCtx;
declare const actionCtx: ActionCtx;

export const _actionUploadUrl: Promise<string> = actionCtx.storage.generateUploadUrl();
export const _actionStorageUrl: Promise<string | null> = actionCtx.storage.getUrl('storage-id' as import('pbvex/server').StorageId);
export const _actionCapabilityStorageUrl: Promise<string | null> = actionCtx.storage.getUrl('storage-id' as import('pbvex/server').StorageId, { mode: 'capability' });
export const _actionPublicStorageUrl: Promise<string | null> = actionCtx.storage.getUrl('storage-id' as import('pbvex/server').StorageId, { mode: 'public' });
// @ts-expect-error storage URL options require an explicit mode
export const _actionMissingStorageUrlMode = actionCtx.storage.getUrl('storage-id' as import('pbvex/server').StorageId, {});
// @ts-expect-error invalid storage URL mode
export const _actionInvalidStorageUrl = actionCtx.storage.getUrl('storage-id' as import('pbvex/server').StorageId, { mode: 'shareable' });
export const _actionStorageDelete: Promise<void> = actionCtx.storage.delete('storage-id' as import('pbvex/server').StorageId);
export const _actionScheduler = actionCtx.scheduler;
// @ts-expect-error actions do not expose direct database access
export const _actionDatabaseQuery = actionCtx.db.query('users');

// @ts-expect-error
export const _wrongNormalizeId: Id<'users'> = queryCtx.db.normalizeId('messages', 'uid');

export const _user: Doc<'users'> = {
  _id: _userId,
  _creationTime: 0,
  name: 'x',
  email: 'x',
  role: 'admin',
  profile: { name: 'x', age: 1 },
};

export const _message: Doc<'messages'> = {
  _id: v.id('messages').validate('mid'),
  _creationTime: 0,
  body: 'x',
  author: _userId,
  channel: 'x',
  priority: 'normal',
};

// @ts-expect-error
export const _badUserDoc: Doc<'users'> = { _id: _userId, _creationTime: 0, name: 'x', email: 'x', role: 'super' };
// @ts-expect-error
export const _missingMessageAuthor: Doc<'messages'> = { _id: v.id('messages').validate('mid'), _creationTime: 0, body: 'x' };
// @ts-expect-error
export const _badMessageAuthor: Doc<'messages'> = { _id: v.id('messages').validate('mid'), _creationTime: 0, body: 'x', author: 'uid' };

export const _filtered = queryCtx.db.query('users').filter((q) => q.eq(q.field('name'), 'x')).first();
export const _filteredLogical = queryCtx.db
  .query('users')
  .filter((q) => q.and(q.eq(q.field('name'), 'x'), q.not(q.eq(q.field('email'), 'y'))))
  .first();
export const _filteredNested = queryCtx.db
  .query('users')
  .filter((q) => q.eq(q.field('profile.name'), 'x'))
  .first();
export const _filteredNestedAge: Promise<Doc<'users'> | null> = queryCtx.db
  .query('users')
  .filter((q) => q.gt(q.field('profile.age'), 3))
  .first();
export const _filteredNestedObject = queryCtx.db.query('users').filter((q) => q.eq(q.field('profile'), _user.profile)).first();
export const _indexed = queryCtx.db.query('users').withIndex('by_email', (q) => q.eq('email', 'x')).first();
export const _nestedIndexed = queryCtx.db.query('users').withIndex('by_profile_name', (q) => q.eq('profile.name', 'x')).first();
export const _ordered = queryCtx.db.query('users').order('asc').first();

// @ts-expect-error raw field-name strings are not valid comparison operands
export const _badFilterField = queryCtx.db.query('users').filter((q) => q.eq(q.field('bad'), 'x'));
// @ts-expect-error comparison value must match the field type
export const _badFilterValue = queryCtx.db.query('users').filter((q) => q.eq(q.field('name'), 1));
// @ts-expect-error logicals require Expression<boolean>, not raw booleans
export const _badFilterBoolean = queryCtx.db.query('users').filter((q) => q.and(true, q.eq(q.field('name'), 'x')));
// @ts-expect-error raw field-name strings are not valid comparison operands
export const _badFilterRawString = queryCtx.db.query('users').filter((q) => q.eq('name', 'x'));
// @ts-expect-error nested path must resolve to an existing field
export const _badFilterNestedMissing = queryCtx.db.query('users').filter((q) => q.eq(q.field('profile.missing'), 'x'));
// @ts-expect-error cannot descend into a non-object leaf field
export const _badFilterNestedLeaf = queryCtx.db.query('users').filter((q) => q.eq(q.field('name.child'), 'x'));
// @ts-expect-error nested path value must match the resolved type
export const _badFilterNestedValue = queryCtx.db.query('users').filter((q) => q.eq(q.field('profile.name'), 1));
// @ts-expect-error index name must exist on the table
export const _badIndexName = queryCtx.db.query('users').withIndex('by_name', (q) => q.eq('email', 'x'));
// @ts-expect-error index field must match the bound index definition
export const _badIndexField = queryCtx.db.query('users').withIndex('by_email', (q) => q.eq('name', 'x'));
// @ts-expect-error messages table has no indexes
export const _noIndex = queryCtx.db.query('messages').withIndex('by_email', (q) => q.eq('email', 'x'));

// @ts-expect-error
export const _badInsertTable = mutationCtx.db.insert('messages', { name: 'x' });
// @ts-expect-error
export const _badInsertValue = mutationCtx.db.insert('users', { name: 'x', email: 'x', role: 'admin', bad: 1 });
// @ts-expect-error
export const _badInsertAuthor = mutationCtx.db.insert('messages', { body: 'x', author: 'uid' });

export const _goodInsert: Promise<Id<'users'>> = mutationCtx.db.insert('users', { name: 'x', email: 'x', role: 'admin', profile: { name: 'x', age: 1 } });

// @ts-expect-error
export const _badPatchField = mutationCtx.db.patch(_userId, { name: 1 });
// @ts-expect-error
export const _badPatchTable = mutationCtx.db.patch(v.id('messages').validate('mid'), { name: 'x' });
// @ts-expect-error
export const _patchSystemField = mutationCtx.db.patch(_userId, { _id: v.id('users').validate('other') });

export const _goodPatch = mutationCtx.db.patch(_userId, { name: 'y' });

// @ts-expect-error
export const _badReplace = mutationCtx.db.replace(_userId, { name: 'x' });

export const _goodReplace = mutationCtx.db.replace(_userId, { name: 'x', email: 'x', role: 'admin', profile: { name: 'x', age: 1 } });

// @ts-expect-error
export const _badRunArgs: Promise<string[]> = actionCtx.run(api.messages.list, { channel: 1 });
// @ts-expect-error
export const _badRunMissingArgs: Promise<string[]> = actionCtx.run(api.messages.list);
// @ts-expect-error
export const _badRunExtraArgs: Promise<string[]> = actionCtx.run(api.messages.list, { channel: 'x', extra: 1 });

export const _noArgsCall: Promise<number> = actionCtx.run(internal.messages.internalStats, {});
// @ts-expect-error
export const _noArgsWithExtra: Promise<number> = actionCtx.run(internal.messages.internalStats, { x: 1 });

declare const _httpRef: import('pbvex/server').FunctionReference<'httpAction', any, any>;
// @ts-expect-error httpAction references cannot be run from action ctx
export const _badRunHttp: Promise<any> = actionCtx.run(_httpRef, {});

export const _scheduledMutation = mutationCtx.scheduler.runAfter(
  1000,
  api.messages.send,
  { channel: 'x', body: 'scheduled' },
);
export const _scheduledAction = actionCtx.scheduler.runAt(
  Date.now() + 1000,
  api.users.search,
  { q: 'x' },
);
declare const scheduledJobId: JobId;
export const _cancelScheduled = mutationCtx.scheduler.cancel(scheduledJobId);
// @ts-expect-error queries are not schedulable
export const _badScheduledQuery = mutationCtx.scheduler.runAfter(0, api.messages.list, { channel: 'x' });
// @ts-expect-error scheduled mutation args are required and typed
export const _badScheduledArgs = actionCtx.scheduler.runAfter(0, api.messages.send, { channel: 1, body: 'x' });
// @ts-expect-error cancel requires a scheduler-issued JobId
export const _badCancelJob = actionCtx.scheduler.cancel('job-id');
