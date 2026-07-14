import { useCallback } from 'react';
import { usePBVexClient } from './useClient.js';
export function useMutation(ref) {
    const client = usePBVexClient();
    return useCallback((args) => client.mutation(ref, args), [client, ref]);
}
//# sourceMappingURL=useMutation.js.map