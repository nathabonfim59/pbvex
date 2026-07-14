// Compile-only type shim for the example fixtures.
// The real runtime implementation lives in @pbvex/sdk-svelte.

export declare function useQuery<T = unknown>(query: unknown): T;
export declare function useMutation<T = unknown, Args extends unknown[] = unknown[]>(query: unknown): (...args: Args) => Promise<T>;
export declare function useSubscription<T = unknown>(query: unknown): T;
