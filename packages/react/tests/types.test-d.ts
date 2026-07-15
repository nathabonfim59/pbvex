import { expectTypeOf } from 'vitest';
import type { FunctionReference, QueryResult } from '@pbvex/client';
import { useQuery, useQueryResult, useMutation, useAction, useSubscription } from '../src/index.js';

// Required args
const queryRef = { _path: 'math:add', _type: 'query' } as FunctionReference<
  'query',
  { a: number; b: number },
  { sum: number },
  'public'
>;

const mutationRef = { _path: 'tasks:update', _type: 'mutation' } as FunctionReference<
  'mutation',
  { id: string; done: boolean },
  { id: string },
  'public'
>;

const actionRef = { _path: 'tasks:notify', _type: 'action' } as FunctionReference<
  'action',
  { taskId: string },
  { sent: boolean },
  'public'
>;

// Empty/undefined args
const noArgsQueryRef = { _path: 'health:status', _type: 'query' } as FunctionReference<
  'query',
  undefined,
  { ok: boolean },
  'public'
>;

const emptyArgsQueryRef = { _path: 'health:status', _type: 'query' } as FunctionReference<
  'query',
  {},
  { ok: boolean },
  'public'
>;

const emptyArgsMutationRef = { _path: 'tasks:reset', _type: 'mutation' } as FunctionReference<
  'mutation',
  {},
  { reset: boolean },
  'public'
>;

// All-optional-but-nonempty args (omittable via empty-object assignability, or supply fields)
const allOptionalArgsQueryRef = { _path: 'tasks:list', _type: 'query' } as FunctionReference<
  'query',
  { filter?: string },
  { tasks: string[] },
  'public'
>;

const allOptionalArgsMutationRef = { _path: 'tasks:touch', _type: 'mutation' } as FunctionReference<
  'mutation',
  { filter?: string },
  { touched: boolean },
  'public'
>;

// Positive query cases
expectTypeOf(useQuery(queryRef, { a: 1, b: 2 })).toEqualTypeOf<{ sum: number } | undefined>();
expectTypeOf(useQuery(queryRef, 'skip')).toEqualTypeOf<{ sum: number } | undefined>();
expectTypeOf(useQuery(noArgsQueryRef)).toEqualTypeOf<{ ok: boolean } | undefined>();
expectTypeOf(useQuery(noArgsQueryRef, 'skip')).toEqualTypeOf<{ ok: boolean } | undefined>();
expectTypeOf(useQuery(emptyArgsQueryRef)).toEqualTypeOf<{ ok: boolean } | undefined>();
expectTypeOf(useQuery(emptyArgsQueryRef, {})).toEqualTypeOf<{ ok: boolean } | undefined>();

// All-optional-but-nonempty query args may be supplied or omitted.
expectTypeOf(useQuery(allOptionalArgsQueryRef, {})).toEqualTypeOf<
  { tasks: string[] } | undefined
>();
expectTypeOf(useQuery(allOptionalArgsQueryRef, { filter: 'x' })).toEqualTypeOf<
  { tasks: string[] } | undefined
>();

expectTypeOf(useQueryResult(queryRef, { a: 1, b: 2 })).toEqualTypeOf<QueryResult<{ sum: number }>>();
expectTypeOf(useSubscription(queryRef, { a: 1, b: 2 })).toEqualTypeOf<
  { sum: number } | undefined
>();

// Positive mutation/action cases
expectTypeOf(useMutation(mutationRef)).toEqualTypeOf<
  (args: { id: string; done: boolean }) => Promise<{ id: string }>
>();
expectTypeOf(useMutation(emptyArgsMutationRef)).toEqualTypeOf<
  (args?: {}) => Promise<{ reset: boolean }>
>();
expectTypeOf(useAction(actionRef)).toEqualTypeOf<
  (args: { taskId: string }) => Promise<{ sent: boolean }>
>();

// All-optional-but-nonempty mutation args may be supplied or omitted.
const mutateAllOptional = useMutation(allOptionalArgsMutationRef);
expectTypeOf(mutateAllOptional({})).toEqualTypeOf<Promise<{ touched: boolean }>>();
expectTypeOf(mutateAllOptional({ filter: 'x' })).toEqualTypeOf<Promise<{ touched: boolean }>>();

// Query hooks must reject mutation/action refs
// @ts-expect-error
useQuery(mutationRef, { id: 'x', done: true });
// @ts-expect-error
useQueryResult(mutationRef, { id: 'x', done: true });
// @ts-expect-error
useSubscription(mutationRef, { id: 'x', done: true });

// Mutation/action must reject query refs
// @ts-expect-error
useMutation(queryRef);
// @ts-expect-error
useAction(queryRef);

// Missing required args for query hooks
// @ts-expect-error
useQuery(queryRef);
// @ts-expect-error
useQueryResult(queryRef);
// @ts-expect-error
useSubscription(queryRef);

// All-optional-but-nonempty query args are omittable (empty-object assignable)
expectTypeOf(useQuery(allOptionalArgsQueryRef)).toEqualTypeOf<{ tasks: string[] } | undefined>();
expectTypeOf(useQueryResult(allOptionalArgsQueryRef)).toEqualTypeOf<QueryResult<{ tasks: string[] }>>();
expectTypeOf(useSubscription(allOptionalArgsQueryRef)).toEqualTypeOf<
  { tasks: string[] } | undefined
>();

// Wrong query arg types
// @ts-expect-error
useQuery(queryRef, { a: '1', b: 2 });
// @ts-expect-error
useQuery(queryRef, { a: 1 });
// @ts-expect-error
useQuery(queryRef, { b: 2 });

// 'skip' is not accepted by mutation/action callables
const mutate = useMutation(mutationRef);
// @ts-expect-error
mutate('skip');
// @ts-expect-error
mutate({ id: 1, done: true });
// @ts-expect-error
mutate({ id: 'x' });

const act = useAction(actionRef);
// @ts-expect-error
act('skip');
// @ts-expect-error
act({ taskId: 1 });
// @ts-expect-error
act();

// All-optional-but-nonempty mutation args are omittable (empty-object assignable)
expectTypeOf(mutateAllOptional()).toEqualTypeOf<Promise<{ touched: boolean }>>();
