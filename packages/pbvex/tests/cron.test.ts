import { describe, expect, it } from 'vitest';
import type { FunctionReference } from '@pbvex/protocol';
import { cronJobs, isCronJobsDefinition } from '../src/runtime/server.js';

const cleanup = {
  _path: 'pbvex_tasks_cleanup_abc',
  _type: 'mutation',
  _visibility: 'internal',
  __noArgs: true,
} as FunctionReference<'mutation', Record<string, never>, null, 'internal'>;

describe('cronJobs', () => {
  it('builds PocketBase-compatible recurring definitions', () => {
    const crons = cronJobs().cron('hourly-cleanup', '@hourly', cleanup);
    expect(isCronJobsDefinition(crons)).toBe(true);
    expect(crons.jobs).toEqual([{
      name: 'hourly-cleanup',
      schedule: '@hourly',
      functionName: 'pbvex_tasks_cleanup_abc',
      args: {},
    }]);
  });

  it('wire-encodes non-JSON PBVex arguments at build time', () => {
    const target = {
      _path: 'pbvex_tasks_rollup_abc',
      _type: 'action',
      _visibility: 'internal',
    } as FunctionReference<'action', { cursor: bigint }, null, 'internal'>;
    const crons = cronJobs().cron('rollup', '@daily', target, { cursor: 1n });
    expect(crons.jobs[0]?.args).toEqual({ cursor: { $integer: 'AQAAAAAAAAA=' } });
  });

  it('rejects duplicate names, invalid expressions, and non-schedulable references', () => {
    expect(() => cronJobs().cron('Bad Name', '@hourly', cleanup)).toThrow(/name/);
    expect(() => cronJobs().cron('cleanup', '@reboot', cleanup)).toThrow(/expression/);
    const crons = cronJobs().cron('cleanup', '@hourly', cleanup);
    expect(() => crons.cron('cleanup', '@daily', cleanup)).toThrow(/Duplicate/);
    const query = { _path: 'read', _type: 'query', _visibility: 'internal' } as never;
    expect(() => cronJobs().cron('read', '@hourly', query)).toThrow(/mutation or action/);
  });
});
