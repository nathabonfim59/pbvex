import { defineComponent, defineSchema, defineTable } from 'pbvex/server';
import { v } from 'pbvex/values';

export const counter = defineComponent({
  modulePaths: ['store.ts'],
  schema: defineSchema({
    counters: defineTable({ value: v.number() }),
  }),
});
