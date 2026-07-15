// Compile-only type shim for the example fixtures.
// The real runtime implementation lives in @pbvex/client.

export type { PbvexValue, Id, JSONValue } from '@pbvex/protocol';

export declare class Client {
  query<T = unknown>(name: string, ...args: unknown[]): Promise<T>;
  mutation<T = unknown>(name: string, ...args: unknown[]): Promise<T>;
  action<T = unknown>(name: string, ...args: unknown[]): Promise<T>;
}

export declare class Query<T = unknown, Args extends unknown[] = unknown[]> {
  name: string;
  constructor(name: string);
  run(...args: Args): Promise<T>;
}

export declare class Mutation<T = unknown, Args extends unknown[] = unknown[]> {
  name: string;
  constructor(name: string);
  run(...args: Args): Promise<T>;
}

export declare class Action<T = unknown, Args extends unknown[] = unknown[]> {
  name: string;
  constructor(name: string);
  run(...args: Args): Promise<T>;
}

export declare class Realtime {
  subscribe<T = unknown>(name: string, callback: (value: T) => void): () => void;
}

export declare function query<T = unknown, Args extends unknown[] = unknown[]>(
  name: string,
  handler: (...args: Args) => Promise<T> | T
): Query<T, Args>;

export declare function mutation<T = unknown, Args extends unknown[] = unknown[]>(
  name: string,
  handler: (...args: Args) => Promise<T> | T
): Mutation<T, Args>;

export declare function action<T = unknown, Args extends unknown[] = unknown[]>(
  name: string,
  handler: (...args: Args) => Promise<T> | T
): Action<T, Args>;

export declare const api: <R extends Record<string, unknown>>(registrar: R) => R;
