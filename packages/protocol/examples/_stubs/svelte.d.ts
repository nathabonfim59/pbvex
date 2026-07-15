// Compile-only type shim for the example fixtures.
// The real runtime implementation lives in @pbvex/svelte.

export interface QueryState<T> {
  readonly data: T | undefined;
  readonly error: Error | null;
  readonly isLoading: boolean;
}

export declare function useQuery<T = unknown>(query: unknown): QueryState<T>;
export declare function useMutation<T = unknown, Args extends unknown[] = unknown[]>(query: unknown): (...args: Args) => Promise<T>;
/** @deprecated Use useQuery. */
export declare const useSubscription: typeof useQuery;
