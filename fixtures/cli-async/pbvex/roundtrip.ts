import { query } from 'pbvex/server';
import { v } from 'pbvex/values';

export const roundtrip = query({
  args: { integer: v.any(), bytes: v.any() },
  returns: v.any(),
  handler: async function (ctx, args) {
    if (!ctx || arguments.length !== 2) throw new Error('expected ctx,args');
    await Promise.resolve();
    return { integer: args.integer, bytes: args.bytes };
  },
});
