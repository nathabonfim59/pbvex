import { defineSchema, defineTable, index } from 'pbvex/server';
import { v } from 'pbvex/values';

export default defineSchema({
  users: defineTable(
    {
      name: v.string(),
      email: v.string(),
      role: v.union(v.literal('admin'), v.literal('user')),
      profile: v.object({
        name: v.string(),
        age: v.number(),
      }),
    },
    {
      indexes: [index('by_email', ['email']), index('by_profile_name', ['profile.name'])],
    },
  ),
  messages: defineTable({
    body: v.string(),
    author: v.id('users'),
    channel: v.optional(v.string()),
    priority: v.defaulted(v.string(), 'normal'),
  }),
});
