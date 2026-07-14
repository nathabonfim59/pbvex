import { useCallback } from 'react';
import { usePBVexClient } from './useClient.js';
export function useAction(ref) {
    const client = usePBVexClient();
    return useCallback((args) => client.action(ref, args), [client, ref]);
}
//# sourceMappingURL=useAction.js.map