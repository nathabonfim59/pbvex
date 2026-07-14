import { defineComponentFns } from 'pbvex/server';
import { v } from 'pbvex/values';
import { counter } from './component';

const component = defineComponentFns(counter);

export const count = component.query({
  args: {},
  returns: v.number(),
  handler: async () => 0,
});
