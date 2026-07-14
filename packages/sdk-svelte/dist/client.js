import { getContext, setContext } from 'svelte';
const CLIENT_CONTEXT_KEY = Symbol('pbvex-client');
export function setClient(client) {
    setContext(CLIENT_CONTEXT_KEY, client);
    return client;
}
export function getClient() {
    const client = getContext(CLIENT_CONTEXT_KEY);
    if (!client) {
        throw new Error('No PBVex client found in Svelte context. Call setClient(client) in a parent component or pass a client explicitly.');
    }
    return client;
}
//# sourceMappingURL=client.js.map