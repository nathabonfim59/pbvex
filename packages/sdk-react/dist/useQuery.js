import { useCallback, useMemo, useSyncExternalStore } from 'react';
import { usePBVexClient } from './useClient.js';
import { QueryStore, toCanonical } from './store.js';
export function useQueryResult(ref, ...args) {
    const client = usePBVexClient();
    const path = ref._path;
    const [argsOrSkip] = args;
    const isSkip = argsOrSkip === 'skip';
    const canonical = useMemo(() => (isSkip ? 'skip' : toCanonical(argsOrSkip)), [isSkip, argsOrSkip]);
    const store = useMemo(() => new QueryStore(client, path, isSkip ? 'skip' : argsOrSkip), [client, path, canonical, isSkip]);
    const subscribe = useCallback((callback) => store.subscribe(callback), [store]);
    const getSnapshot = useCallback(() => store.getSnapshot(), [store]);
    const getServerSnapshot = useCallback(() => store.getServerSnapshot(), [store]);
    return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
export function useQuery(ref, ...args) {
    const result = useQueryResult(ref, ...args);
    if (result.error) {
        throw result.error;
    }
    if (result.isLoading) {
        return undefined;
    }
    return result.data;
}
//# sourceMappingURL=useQuery.js.map