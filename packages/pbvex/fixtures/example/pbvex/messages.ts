import { query, mutation, internalQuery } from 'pbvex/server';
import type { GenericId } from 'pbvex/server';
import { v } from 'pbvex/values';

export const list = query({
  args: { channel: v.string() },
  returns: v.array(v.string()),
  handler: async (ctx, args) => {
    return [];
  },
});

export const send = mutation({
  args: { channel: v.string(), body: v.string() },
  returns: v.id('messages'),
  handler: async (ctx, args) => {
    return 'placeholder' as GenericId<'messages'>;
  },
});

export const internalStats = internalQuery({
  args: {},
  returns: v.number(),
  handler: async (ctx, args) => {
    return 0;
  },
});
