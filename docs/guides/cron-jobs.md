# Cron jobs

Use cron jobs for recurring application-wide work with a fixed calendar schedule: periodic cleanup, reports, synchronization, rollups, and maintenance. Use [one-shot scheduling](./scheduling.md) instead when a job belongs to a specific record, waits for an elapsed delay, or runs at a dynamically calculated instant.

PocketBase provides each calendar tick, and PBVex enqueues that occurrence into its durable job worker. Cron invocations therefore use the same immutable deployment snapshots, concurrency controls, status history, retries, and idempotency rules as one-shot scheduled jobs.

## Define recurring jobs

Create `pbvex/crons.ts` and export exactly one `cronJobs()` definition as the module default:

```ts
// pbvex/crons.ts
import { cronJobs } from 'pbvex/server';
import { internal } from './_generated/api';

const crons = cronJobs();

crons.cron(
  'hourly-expired-session-cleanup',
  '@hourly',
  internal.maintenance.removeExpiredSessions,
  { batchSize: 100 },
);

crons.cron(
  'weekday-summary',
  '0 9 * * 1-5',
  internal.reports.sendDailySummary,
);

export default crons;
```

Define the target function, run `pbvex codegen`, and then use its generated reference in `crons.ts`. The target must be a public or internal mutation/action; queries and HTTP actions cannot be cron targets. Prefer internal functions for operational work that clients should not invoke.

The generated reference preserves argument types and autocomplete. Missing arguments, incorrect field types, and unsupported target kinds fail TypeScript checking. PBVex validates the same contract again when building and activating the deployment.

Cron names must match `[a-z][a-z0-9_-]{0,63}`, must be unique, and should describe the job rather than repeat its schedule. A deployment may declare up to 64 cron jobs.

## Cron expression format

PocketBase uses five space-separated fields evaluated in UTC with one-minute precision:

```text
┌───────────── minute (0-59)
│ ┌─────────── hour (0-23)
│ │ ┌───────── day of month (1-31)
│ │ │ ┌─────── month (1-12)
│ │ │ │ ┌───── day of week (0-6, Sunday is 0)
│ │ │ │ │
* * * * *
```

Fields support wildcards (`*`), lists (`1,3,5`), ranges (`1-5`), steps (`*/10`), and stepped ranges (`10-30/5`). Month and weekday names such as `JAN` or `MON` are not supported. When both day-of-month and day-of-week are restricted, PocketBase requires both fields to match.

Supported macros are `@yearly`/`@annually`, `@monthly`, `@weekly`, `@daily`/`@midnight`, and `@hourly`.

## Common schedules

| Schedule | Expression | Meaning in UTC |
| --- | --- | --- |
| Every minute | `* * * * *` | At every minute boundary |
| Every 5 minutes | `*/5 * * * *` | Minutes 0, 5, 10, …, 55 |
| Every 15 minutes | `*/15 * * * *` | Minutes 0, 15, 30, and 45 |
| Hourly | `@hourly` | Minute 0 of every hour |
| Every 6 hours | `0 */6 * * *` | 00:00, 06:00, 12:00, and 18:00 |
| Daily at midnight | `@daily` | 00:00 every day |
| Daily at 09:00 | `0 9 * * *` | 09:00 every day |
| Weekdays at 09:00 | `0 9 * * 1-5` | Monday through Friday at 09:00 |
| Weekly | `@weekly` | Sunday at 00:00 |
| Monthly | `@monthly` | First day of the month at 00:00 |
| Yearly | `@yearly` | January 1 at 00:00 |

PBVex intentionally leaves the PocketBase app cron timezone at UTC. If a business schedule must follow a user’s changing local time zone or daylight-saving rules, calculate the next exact instant and use [`runAt`](./scheduling.md#schedule-at-a-date-and-time), or use an external timezone-aware scheduler.

## Choosing cron or one-shot scheduling

| Requirement | Use | Why |
| --- | --- | --- |
| Clean expired sessions every hour | Cron | Fixed application-wide cadence |
| Send a weekday report at 09:00 UTC | Cron | Fixed calendar schedule |
| Remind each user at their chosen instant | `runAt` | Each record has a different timestamp |
| Retry a workflow after five minutes | `runAfter` | The delay is relative to the current attempt |
| Run again based on the previous result | Self-schedule with `runAfter`/`runAt` | The next occurrence is dynamic |
| Follow 09:00 in each user’s local timezone | Calculate an instant and use `runAt` | PocketBase cron is UTC and application-wide |

## Execution behavior

Each PocketBase tick immediately enqueues an ordinary durable PBVex job. Keep these consequences in mind:

- Ticks missed while the server is stopped are not backfilled automatically.
- A new tick can be enqueued while the previous occurrence is still running.
- The cron time is the earliest enqueue time, not a strict execution deadline.
- Delivery is at least once, so every occurrence must be idempotent and safe to overlap or retry.
- Cron jobs run without a user authentication/session context.
- An occurrence already enqueued remains pinned to the deployment that produced it.

Activating a deployment replaces the previous PBVex cron registrations. Rolling back restores the selected deployment’s definitions. Existing non-PBVex PocketBase cron jobs are left untouched.

## Inspect and trigger cron jobs

PBVex registrations appear in **PocketBase Dashboard → Settings → Crons** with IDs such as `pbvex:hourly-expired-session-cleanup`. A superuser can inspect them through `GET /api/crons` and manually trigger one with `POST /api/crons/{id}`. A manual trigger follows the same path and enqueues a durable PBVex job.

Monitor the resulting execution through the PBVex scheduler administration endpoints described in [Scheduler operations](../self-hosting.md#scheduler-operations).

## When self-scheduling is useful

Use `pbvex/crons.ts` for a fixed application-wide calendar. Self-schedule when each record has its own cadence, the next occurrence depends on the previous result, or the schedule is stored as application data:

```ts
import { HOUR_MS } from 'pbvex/server';

export const sweep = internalMutation({
  args: {},
  handler: async (ctx) => {
    // Process a bounded batch here.
    await ctx.scheduler.runAfter(
      HOUR_MS,
      internal.maintenance.sweep,
      {},
    );
  },
});
```

This measures from the end of one run, so execution time causes drift. It also stops recurring if the handler reaches a terminal failure before scheduling the next job.

For a wall-clock self-scheduling loop, calculate the next intended instant and use `runAt` instead of adding `DAY_MS`. That avoids accumulating execution-time drift, but the application must still handle time zones and daylight-saving rules. See [Scheduling one-shot work](./scheduling.md) for durability, retry, cancellation, and idempotency details shared by all PBVex jobs.

## Operational checklist

- Prefer internal mutation/action targets.
- Keep every occurrence small and bounded; chain batches for large datasets.
- Re-read current records instead of trusting stale arguments.
- Make database writes and external calls idempotent.
- Prevent or safely tolerate overlapping occurrences.
- Treat cron times as earliest enqueue times, not execution deadlines.
- Inspect registrations in PocketBase and executions through PBVex scheduler operations.
- Remember the single-node boundary documented in [Limits and boundaries](./limits.md).
