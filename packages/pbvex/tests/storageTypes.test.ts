import { describe, expectTypeOf, it } from 'vitest';

import type { ActionCtx, QueryCtx, SchedulerContext, StorageContext, StorageId, StorageReader } from '../src/runtime/server.js';

describe('storage authoring types', () => {
  it('uses branded ids and nullable getUrl results', () => {
    expectTypeOf<Parameters<StorageContext['getUrl']>[0]>().toEqualTypeOf<StorageId>();
    expectTypeOf<Awaited<ReturnType<StorageContext['getUrl']>>>().toEqualTypeOf<string | null>();
    expectTypeOf<Parameters<StorageContext['delete']>[0]>().toEqualTypeOf<StorageId>();
    expectTypeOf<QueryCtx['storage']>().toEqualTypeOf<StorageReader>();
    expectTypeOf<ActionCtx['storage']>().toEqualTypeOf<StorageContext>();
    expectTypeOf<ActionCtx['scheduler']>().toEqualTypeOf<SchedulerContext>();
    expectTypeOf<ActionCtx['db']>().toEqualTypeOf<undefined>();
  });
});
