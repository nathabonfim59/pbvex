import type { FunctionReference } from '@pbvex/protocol';
import { cronJobs } from '../src/runtime/server.js';

declare const mutation: FunctionReference<'mutation', { accountId: string }, null, 'internal'>;
declare const actionWithoutArgs: FunctionReference<'action', Record<string, never>, null, 'internal'>;
declare const query: FunctionReference<'query', Record<string, never>, null, 'internal'>;

const crons = cronJobs();
crons.cron('account-cleanup', '0 3 * * *', mutation, { accountId: 'account_1' });
crons.cron('heartbeat', '@hourly', actionWithoutArgs);

// @ts-expect-error required target arguments are missing
crons.cron('missing-args', '@daily', mutation);
// @ts-expect-error target arguments retain generated types
crons.cron('wrong-args', '@daily', mutation, { accountId: 123 });
// @ts-expect-error queries cannot be recurring targets
crons.cron('read', '@daily', query);
