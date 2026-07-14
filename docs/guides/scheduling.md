# Scheduling work

Mutations and actions can schedule a public or internal mutation/action with `ctx.scheduler`. Queries and HTTP actions are not schedulable.

```ts
// pbvex/reminders.ts
import { mutation, internalMutation } from './_generated/server';
import { internal } from './_generated/api';
import { v } from 'pbvex/values';

export const enqueue = mutation({
  args: { message: v.string() },
  returns: v.string(),
  handler: async (ctx, { message }) => {
    const id = await ctx.scheduler.runAfter(5_000, internal.reminders.deliver, { message });
    return id;
  },
});

export const deliver = internalMutation({
  args: { message: v.string() },
  handler: async (_ctx, { message }) => {
    console.log(message);
  },
});
```

`runAfter(delayMs, ref, args?)` uses a non-negative integer delay and rejects delays over five years. `runAt(when, ref, args?)` accepts a `Date` or an integer epoch milliseconds timestamp; it rejects times more than one minute in the past or more than 100 years in the future. Both return an opaque `JobId`. Generated references enforce the target kind and argument shape at compile time; runtime validates the reference again.

```ts
const id = await ctx.scheduler.runAt(Date.now() + 60_000, internal.reminders.deliver, {
  message: 'one minute elapsed',
});
await ctx.scheduler.cancel(id);
```

## Cancellation and ownership

`cancel(jobId)` can cancel a pending or running job. It is idempotence-sensitive: a completed, failed, or already canceled job cannot be canceled. A component can cancel only jobs scheduled by the same component namespace; root functions are similarly isolated. Cancellation requests a running attempt stop, so handlers should keep externally visible work idempotent and honor normal request cancellation indirectly through the runtime.

There is no function-level API to inspect a job or retry it. Superuser-only operator endpoints provide status, cancellation, and retry; see [the operator guide](../operator.md#scheduler-operations).

## Durability, transactions, and retries

Scheduling is durable. A job stores the immutable deployment snapshot it targets, so later activation does not redirect already scheduled work. Jobs survive process restart; workers recover expired leases. If `runAfter`/`runAt` runs inside a mutation transaction, its durable record participates in that transaction: a rolled-back mutation does not leave the schedule behind.

Delivery is at least once, not exactly once. A lease expiry or process failure can lead to another attempt, and failed jobs are retried with bounded exponential backoff (the default maximum is five attempts). Make effects idempotent: store a deduplication key in a mutation before sending email, charging, or calling a remote system. A finished job is retained for a bounded history period, then cleanup may remove it.

Scheduling itself does not make an action and a later mutation atomic. A scheduled action has no direct database access; it must call a mutation for durable writes. The scheduled invocation receives its saved arguments, but it is not a continuation of the originating request and should not assume its caller’s live auth/session context.
