import { useQuery } from './useQuery.js';
/**
 * Backwards-compatible alias for useQuery.
 */
export function useSubscription(ref, ...args) {
    return useQuery(ref, ...args);
}
//# sourceMappingURL=useSubscription.js.map