import { describe, it, expectTypeOf, assertType } from 'vitest';
import {
  useQuery,
  useQueryResult,
  useSubscription,
  createQuery,
  useMutation,
  useAction,
  skip,
  type QueryState,
  type Skip,
} from '../src/index.js';
import { Client, type FunctionReference, type QueryResult } from '@pbvex/client';
import { MockClient, addRef, emptyRef } from './mockClient.js';

const sumRef = {
  _path: 'math:sum',
  _type: 'mutation',
} as FunctionReference<'mutation', { a: number; b: number }, number, 'public'>;

const greetRef = {
  _path: 'greet:hello',
  _type: 'action',
} as FunctionReference<'action', { name: string }, string, 'public'>;

const emptyMutRef = {
  _path: 'util:empty',
  _type: 'mutation',
} as FunctionReference<'mutation', void, number, 'public'>;

const optionalMutRef = {
  _path: 'util:optional',
  _type: 'mutation',
} as FunctionReference<'mutation', { filter?: string }, number, 'public'>;

const optionalActionRef = {
  _path: 'util:optionalAction',
  _type: 'action',
} as FunctionReference<'action', { filter?: string }, string, 'public'>;

const neverMutRef = {
  _path: 'util:never',
  _type: 'mutation',
  __noArgs: true,
} as FunctionReference<'mutation', never, number, 'public'>;

const client = new MockClient();

describe('type inference', () => {
  it('skip is the literal string "skip"', () => {
    assertType<Skip>(skip);
    expectTypeOf(skip).toEqualTypeOf<'skip'>();
  });

  it('useQuery infers args and return type', () => {
    if (false) {
      const state = useQuery(addRef, { a: 1, b: 2 }, client);
      assertType<QueryState<{ sum: number }>>(state);
      expectTypeOf(state.data).toEqualTypeOf<{ sum: number } | undefined>();
    }
  });

  it('useQueryResult exposes the full QueryResult', () => {
    if (false) {
      const result = useQueryResult(addRef, { a: 1, b: 2 }, client);
      assertType<QueryState<{ sum: number }>>(result);
      expectTypeOf(result.data).toEqualTypeOf<{ sum: number } | undefined>();
      expectTypeOf(result.error).toEqualTypeOf<Error | null>();
      expectTypeOf(result.isLoading).toEqualTypeOf<boolean>();
    }
  });

  it('useSubscription is a compatible alias for useQueryResult', () => {
    if (false) {
      const result = useSubscription(addRef, { a: 1, b: 2 }, client);
      assertType<QueryState<{ sum: number }>>(result);
    }
  });

  it('createQuery is an alias for useQuery', () => {
    if (false) {
      const state = createQuery(addRef, { a: 1, b: 2 }, client);
      assertType<QueryState<{ sum: number }>>(state);
    }
  });

  it('useQuery supports skip and reactive args', () => {
    if (false) {
      assertType<QueryState<{ sum: number }>>(useQuery(addRef, 'skip', client));
      assertType<QueryState<{ sum: number }>>(useQuery(addRef, { a: 1, b: 2 }, client));
      assertType<QueryState<{ sum: number }>>(useQuery(addRef, () => ({ a: 1, b: 2 }), client));
    }
  });

  it('useQuery allows empty-args queries without args or with explicit client', () => {
    if (false) {
      assertType<QueryState<string>>(useQuery(emptyRef, client));
      assertType<QueryState<string>>(useQuery(emptyRef, undefined, client));
    }
  });

  it('useMutation infers args and return', () => {
    const mutate = useMutation(sumRef, client);
    assertType<(args: { a: number; b: number }) => Promise<number>>(mutate);
  });

  it('useAction infers args and return', () => {
    const act = useAction(greetRef, client);
    assertType<(args: { name: string }) => Promise<string>>(act);
  });

  it('empty-args mutation returns optional callable', () => {
    const mutate = useMutation(emptyMutRef, client);
    assertType<(args?: void) => Promise<number>>(mutate);
  });

  it('all-optional and never args return optional callables', () => {
    const mutate = useMutation(optionalMutRef, client);
    const act = useAction(optionalActionRef, client);
    const neverMutate = useMutation(neverMutRef, client);

    assertType<(args?: { filter?: string }) => Promise<number>>(mutate);
    assertType<(args?: { filter?: string }) => Promise<string>>(act);
    assertType<(args?: never) => Promise<number>>(neverMutate);
    void mutate();
    void act();
    void neverMutate();
  });

  it('explicit client overloads accept a Client', () => {
    if (false) {
      const otherClient = new Client('http://localhost:8090');
      assertType<QueryState<{ sum: number }>>(useQuery(addRef, { a: 1, b: 2 }, otherClient));
    }
  });

  it('wrong function-reference kinds are compile-time errors', () => {
    if (false) {
    // @ts-expect-error useQuery requires a query reference
    useQuery(sumRef, { a: 1, b: 2 }, client);
    // @ts-expect-error useMutation requires a mutation reference
    useMutation(addRef, client);
    // @ts-expect-error useAction requires an action reference
    useAction(addRef, client);
    }
  });

  it('required query args cannot be omitted', () => {
    if (false) {
    // @ts-expect-error passing client instead of args
    useQuery(addRef, client);
    // @ts-expect-error undefined is not a valid required-arg value
    useQuery(addRef, undefined, client);
    // @ts-expect-error missing required arg
    useQuery(addRef, { a: 1 }, client);
    // @ts-expect-error wrong arg type
    useQuery(addRef, { a: 1, b: '2' }, client);
    // @ts-expect-error extra arg
    useQuery(addRef, { a: 1, b: 2, c: 3 }, client);
    }
  });

  it('required mutation/action args cannot be omitted', () => {
    const mutate = useMutation(sumRef, client);
    const act = useAction(greetRef, client);
    // @ts-expect-error missing args
    mutate();
    // @ts-expect-error missing required arg
    mutate({ a: 1 });
    // @ts-expect-error wrong arg type
    mutate({ a: 1, b: '2' });
    // @ts-expect-error extra arg
    mutate({ a: 1, b: 2, c: 3 });
    // @ts-expect-error missing args
    act();
    // @ts-expect-error wrong arg type
    act({ name: 1 });
  });
});
