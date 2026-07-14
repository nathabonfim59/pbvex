import { useContext } from 'react';
import { PBVexContext } from './provider.js';
export function usePBVexClient() {
    const client = useContext(PBVexContext);
    if (!client) {
        throw new Error('usePBVexClient must be used within a PBVexProvider');
    }
    return client;
}
//# sourceMappingURL=useClient.js.map