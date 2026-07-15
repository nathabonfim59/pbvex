import test from 'node:test';
import assert from 'node:assert/strict';
import { isCronExpression } from './cron.js';
import { validateManifest } from './manifest.js';

test('PocketBase cron expressions and macros are validated', () => {
  for (const value of ['* * * * *', '*/5 * * * *', '0 9 * * 1-5', '@hourly', '@daily', '@yearly']) {
    assert.equal(isCronExpression(value), true, value);
  }
  for (const value of ['', '* * * *', '* * * * * *', '60 * * * *', '0 24 * * *', '0 0 * * 7', '2/3 * * * *', '@reboot']) {
    assert.equal(isCronExpression(value), false, value);
  }
});

test('manifest cron jobs are sorted and target mutations or actions', () => {
  const base = {
    protocolVersion: 'v1',
    deploymentId: 'cron_test',
    functions: [
      { name: 'cleanup', type: 'mutation', visibility: 'internal', modulePath: 'pbvex/tasks.ts', exportName: 'cleanup', args: { type: 'object', shape: {} }, returns: { type: 'null' } },
      { name: 'read', type: 'query', visibility: 'internal', modulePath: 'pbvex/tasks.ts', exportName: 'read', args: { type: 'object', shape: {} }, returns: { type: 'null' } },
    ],
  } as const;
  const valid = validateManifest({
    ...base,
    cronJobs: [{ name: 'hourly-cleanup', schedule: '@hourly', functionName: 'cleanup', args: {} }],
  });
  assert.equal(valid.cronJobs?.[0]?.schedule, '@hourly');

  assert.throws(() => validateManifest({
    ...base,
    cronJobs: [{ name: 'bad', schedule: '@hourly', functionName: 'read', args: {} }],
  }), /target must be a deployed mutation or action/);
  assert.throws(() => validateManifest({
    ...base,
    cronJobs: [
      { name: 'z-last', schedule: '@daily', functionName: 'cleanup', args: {} },
      { name: 'a-first', schedule: '@hourly', functionName: 'cleanup', args: {} },
    ],
  }), /sorted by name/);
});
