import { getClient } from './client.js';
export function useMutation(ref, client) {
    const resolved = client ?? getClient();
    return ((args) => resolved.mutation(ref, args));
}
export function useAction(ref, client) {
    const resolved = client ?? getClient();
    return ((args) => resolved.action(ref, args));
}
//# sourceMappingURL=useMutation.js.map