import { derived } from 'svelte/store';
import { Client, } from '@pbvex/sdk-core';
import { encodeValue, canonicalJson } from '@pbvex/protocol';
import { getClient } from './client.js';
export const skip = 'skip';
function isReadable(value) {
    return (value !== null &&
        typeof value === 'object' &&
        'subscribe' in value &&
        typeof value.subscribe === 'function');
}
function isSkip(value) {
    return value === 'skip';
}
function normalizeArgs(args) {
    if (args === undefined || isSkip(args) || !isReadable(args)) {
        return { subscribe: (fn) => { fn(args); return () => { }; } };
    }
    return args;
}
function resolveClientAndArgs(argsOrClient, maybeClient) {
    const client = maybeClient ?? (argsOrClient instanceof Client ? argsOrClient : undefined) ?? getClient();
    const args = argsOrClient instanceof Client ? undefined : argsOrClient;
    return { args, client };
}
function defaultEncodeArgs(args) {
    return args === undefined ? {} : encodeValue(args);
}
function argsKey(args) {
    return canonicalJson(defaultEncodeArgs(args));
}
function createQueryResultStore(ref, args, client) {
    const argsStore = normalizeArgs(args);
    const initialResult = { data: undefined, error: null, isLoading: true };
    let value = initialResult;
    const subscribers = new Set();
    let generation = 0;
    let lastKey;
    let argsUnsubscribe;
    let watchUnsubscribe;
    const set = (next) => {
        if (next !== value) {
            value = next;
            for (const fn of subscribers) {
                fn(value);
            }
        }
    };
    function start() {
        argsUnsubscribe = argsStore.subscribe((currentArgs) => {
            const key = isSkip(currentArgs) ? 'skip' : argsKey(currentArgs);
            if (key === lastKey)
                return;
            lastKey = key;
            // Invalidate any callbacks from the previous watch before tearing it down.
            ++generation;
            if (watchUnsubscribe) {
                watchUnsubscribe();
                watchUnsubscribe = undefined;
            }
            set(initialResult);
            if (isSkip(currentArgs)) {
                set({ data: undefined, error: null, isLoading: false });
                return;
            }
            const g = generation;
            watchUnsubscribe = client.watch(ref, currentArgs, {
                onUpdate: (result) => {
                    if (g !== generation)
                        return;
                    set(result);
                },
                onError: (error) => {
                    if (g !== generation)
                        return;
                    set({ data: undefined, error, isLoading: false });
                },
            });
        });
    }
    function stop() {
        ++generation;
        argsUnsubscribe?.();
        watchUnsubscribe?.();
        argsUnsubscribe = undefined;
        watchUnsubscribe = undefined;
        lastKey = undefined;
    }
    return {
        subscribe(fn, invalidate) {
            subscribers.add(fn);
            if (subscribers.size === 1)
                start();
            fn(value);
            return () => {
                subscribers.delete(fn);
                if (subscribers.size === 0)
                    stop();
            };
        },
    };
}
export function useQueryResult(ref, argsOrClient, maybeClient) {
    const { args, client } = resolveClientAndArgs(argsOrClient, maybeClient);
    return createQueryResultStore(ref, args, client);
}
export function useQuery(ref, argsOrClient, maybeClient) {
    const { args, client } = resolveClientAndArgs(argsOrClient, maybeClient);
    return derived(createQueryResultStore(ref, args, client), (result) => result.data);
}
export const createQuery = useQuery;
export const useSubscription = useQueryResult;
//# sourceMappingURL=useQuery.js.map