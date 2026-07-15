import { query, action } from 'pbvex/server';
import { v } from 'pbvex/values';
import { truncate } from './helpers';

export const get = query({
  args: { id: v.id('users') },
  returns: { name: v.string(), email: v.string() },
  handler: async (ctx, args) => {
    return { name: truncate('Unknown', 10), email: '' };
  },
});

export const search = action({
  args: { q: v.string() },
  returns: v.array(v.object({ name: v.string(), email: v.string() })),
  handler: async (ctx, args) => {
    return [];
  },
});
