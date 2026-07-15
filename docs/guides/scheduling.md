# Scheduling one-shot work

Use one-shot durable jobs for reminders, delayed notifications, grace periods, publication times, and user-specific deadlines. For periodic cleanup, reports, synchronization, and other fixed application-wide schedules, see [Cron jobs](./cron-jobs.md).

Jobs execute through the durable PBVex worker and retain deployment snapshots, status history, retries, and concurrency controls.

## What can schedule what

`ctx.scheduler` is available in mutations, actions, and HTTP actions. Queries cannot schedule work because queries are read-only.

The target must be a public or internal mutation/action. A query or HTTP action cannot be a scheduled target.

| Calling function | Can use `ctx.scheduler`? | Can be a target? | Typical role |
| --- | --- | --- | --- |
| Query | No | No | Read data without side effects |
| Mutation | Yes | Yes | Atomically change data and enqueue follow-up work |
| Action | Yes | Yes | Call external services or coordinate longer workflows |
| HTTP action | Yes | No | Accept a webhook or HTTP request and enqueue internal work |

Prefer internal targets unless clients must invoke the same function directly. Generated function references restrict the target kind and validate its arguments at compile time; PBVex validates both again when the job is created.

## Schedule after a delay

Use `runAfter` when the delay matters more than a calendar time. The delay is an integer number of milliseconds.

```ts
// pbvex/reminders.ts
import { internalMutation, mutation } from './_generated/server';
import { internal } from './_generated/api';
import { MINUTE_MS } from 'pbvex/server';
import { v } from 'pbvex/values';

export const create = mutation({
  args: {
    message: v.string(),
  },
  returns: v.string(),
  handler: async (ctx, { message }) => {
    return await ctx.scheduler.runAfter(
      5 * MINUTE_MS,
      internal.reminders.deliver,
      { message },
    );
  },
});

export const deliver = internalMutation({
  args: {
    message: v.string(),
  },
  handler: async (_ctx, { message }) => {
    console.log(message);
  },
});
```

`runAfter(delayMs, functionReference, args?)` accepts delays from `0` through five years and returns an opaque `JobId`. A zero delay means “eligible as soon as possible,” not “run synchronously before this function returns.”

### Common delay values

Import the duration constants from `pbvex/server` instead of embedding unexplained millisecond values:

```ts
import {
  SECOND_MS,
  MINUTE_MS,
  HOUR_MS,
  DAY_MS,
  WEEK_MS,
} from 'pbvex/server';

await ctx.scheduler.runAfter(
  3 * DAY_MS,
  internal.trials.sendExpiryWarning,
  { accountId },
);
```

| Desired delay | Recommended expression | Millisecond value |
| --- | ---: | ---: |
| Immediately/asynchronously | `0` | `0` |
| 5 seconds | `5 * SECOND_MS` | `5_000` |
| 30 seconds | `30 * SECOND_MS` | `30_000` |
| 1 minute | `MINUTE_MS` | `60_000` |
| 5 minutes | `5 * MINUTE_MS` | `300_000` |
| 15 minutes | `15 * MINUTE_MS` | `900_000` |
| 1 hour | `HOUR_MS` | `3_600_000` |
| 6 hours | `6 * HOUR_MS` | `21_600_000` |
| 1 day | `DAY_MS` | `86_400_000` |
| 1 week | `WEEK_MS` | `604_800_000` |
| 30 days | `30 * DAY_MS` | `2_592_000_000` |

These are elapsed durations. A “day” here is exactly 24 hours; it is not necessarily the same local wall-clock time tomorrow across a daylight-saving transition.

## Schedule at a date and time

Use `runAt` for reminders, publication times, reservation expiry, and other work tied to a specific instant.

```ts
const publishAt = new Date('2026-08-01T14:00:00Z');

const jobId = await ctx.scheduler.runAt(
  publishAt,
  internal.posts.publish,
  { postId },
);
```

`runAt(when, functionReference, args?)` accepts either a `Date` or an integer Unix timestamp in milliseconds. Times up to one minute in the past are accepted and become immediately eligible; older timestamps are rejected. The upper limit is 100 years in the future.

Always include an offset or `Z` when parsing a date string. For user-local calendar rules such as “09:00 in São Paulo,” resolve the intended time zone to an exact timestamp before calling `runAt`. Store the user’s IANA time-zone name if you must calculate future occurrences, because a fixed UTC offset does not model daylight-saving changes.

## API reference

| Method | Purpose | Important limits |
| --- | --- | --- |
| `runAfter(delayMs, ref, args?)` | Run once after an elapsed delay | Integer milliseconds; `0` to five years |
| `runAt(when, ref, args?)` | Run once at an absolute instant | `Date` or integer epoch milliseconds; no more than one minute past or 100 years future |
| `cancel(jobId)` | Cancel a pending or running job | Same component owner; terminal jobs cannot be canceled |

All methods are asynchronous. The generated reference determines whether the argument object is required, and its TypeScript type must match the target function’s validators.

## Common use cases

| Use case | Recommended pattern | Why |
| --- | --- | --- |
| Send a welcome email after signup | Mutation schedules an internal action with `runAfter` | Account creation and enqueueing can commit together; the action can use `ctx.email` |
| Remind a user at a chosen time | `runAt` an internal action | Models a specific instant and permits external email/SMS calls |
| Expire a reservation | Creation mutation schedules an internal mutation | The expiry can atomically re-check and update database state |
| Process a webhook outside the response | HTTP action validates the request, then schedules an internal action | Returns quickly and isolates slower downstream calls |
| Debounce work | Store the latest `JobId`, cancel it, then schedule a replacement | Prevents obsolete pending jobs from doing unnecessary work |
| Process a large dataset | Schedule small mutation/action batches that enqueue the next cursor | Keeps each invocation bounded and restartable |

For recurring application-wide work, use a [cron job](./cron-jobs.md) instead of repeatedly scheduling the same fixed interval yourself.

Do not use a scheduled job as a sleep primitive or as a precise timer. Jobs become eligible at their due time, but polling, the default concurrency limit, other queued work, and process downtime can delay their actual start. The default worker polls every two seconds and executes up to five jobs concurrently.

## Choose a mutation or an action target

Use a scheduled mutation when all work is database/storage state that should commit atomically. Mutations can read and write the database, use storage, and enqueue more work.

Use a scheduled action when the job must send email, call an external API, or coordinate other PBVex functions. Actions have no direct database access; call a mutation for durable reads or writes.

```ts
// pbvex/trials.ts
import { internalAction } from './_generated/server';
import { v } from 'pbvex/values';

export const sendExpiryWarning = internalAction({
  args: {
    email: v.string(),
    daysRemaining: v.number(),
  },
  handler: async (ctx, { email, daysRemaining }) => {
    await ctx.email.send({
      template: 'trial-expiry-warning',
      to: email,
      variables: { daysRemaining },
    });
  },
});
```

See [Application email templates](./email-templates.md) for typed template names and variables.

## Cancel or replace pending work

Keep the returned `JobId` when a user may undo or reschedule the operation.

```ts
// pbvex/reminders.ts
import type { JobId } from 'pbvex/server';
import { mutation } from './_generated/server';
import { v } from 'pbvex/values';

export const cancel = mutation({
  args: {
    jobId: v.string(),
  },
  handler: async (ctx, { jobId }) => {
    // The database stores the opaque branded ID as a string. Cast only after
    // validating that this value came from your own scheduler record.
    await ctx.scheduler.cancel(jobId as JobId);
  },
});
```

Cancellation works for pending and running jobs. Canceling a running job requests that its current attempt stop, but code that already reached an external service may still have produced an effect. A completed, failed, canceled, missing, or differently owned job cannot be canceled.

Cancellation is scoped to the component namespace that created the job. A component cannot cancel a root job or another component’s job, even if it learns the ID.

There is no function-level API to inspect or manually retry a job. Superuser-only administrative endpoints expose status, cancellation, and retry; see [Scheduler operations](../self-hosting.md#scheduler-operations).

## Durability and deployment behavior

Scheduled jobs are stored durably and survive a PBVex process restart. If scheduling occurs inside a mutation, the job record participates in the same transaction:

- If the mutation commits, its job is durable.
- If the mutation rolls back, no job is left behind.
- Scheduling from an action or HTTP action is a separate durable operation because those callers do not own a database transaction.

Each job pins the immutable deployment snapshot that created it. Activating newer application code does not redirect an already scheduled job to the new implementation. This makes delayed work reproducible, but old deployments remain retained while their jobs need them.

The job receives its saved arguments, not a continuation of the originating request. Authentication/session context is not preserved. Pass stable record IDs or the minimum non-secret data needed by the target, then re-read current state and authorization-relevant facts when it runs. Never place auth tokens or credentials in scheduled arguments.

## Delivery, retries, and idempotency

Delivery is at least once, not exactly once. A process failure or expired lease can cause another worker attempt, so every handler must tolerate repetition.

PBVex automatically retries eligible infrastructure failures and execution timeouts with bounded exponential backoff and jitter. The defaults allow up to five attempts, beginning around one second and capping retry delay at one minute. A JavaScript exception thrown by the application handler is treated as a terminal failure rather than automatically retried. Operators can manually retry a terminal job while its deployment snapshot still exists.

Make externally visible effects idempotent:

1. Give the operation a stable deduplication key, such as `welcome:${userId}`.
2. In a mutation, record whether that operation has already completed or been claimed.
3. Make the external provider idempotent too when it supports idempotency keys.
4. Record the result before considering the workflow complete.

Scheduling an action and later calling a mutation does not make the external effect and database update atomic. Design for any boundary to be repeated after a crash.

Terminal job history is retained for seven days by default, then cleaned up in bounded batches. Scheduler concurrency, polling, lease, retry, and retention settings are currently Go embedding settings rather than CLI flags.

## Operational checklist

- Keep every job small and bounded; chain batches rather than processing an unbounded collection.
- Use internal targets unless direct client invocation is intentional.
- Re-read current records when the job starts instead of trusting stale snapshots in its arguments.
- Treat due times as earliest eligible times, not deadlines.
- Store a `JobId` when cancellation or replacement is part of the product flow.
- Make database writes and external calls idempotent.
- Monitor failed jobs through the superuser scheduler endpoints.
- Remember the single-node boundary: never run multiple PBVex processes against one data directory. See [Limits and boundaries](./limits.md).
