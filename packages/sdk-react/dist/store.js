import { canonicalJson, encodeValue } from '@pbvex/protocol';
const LOADING = Object.freeze({ data: undefined, error: null, isLoading: true });
const SKIPPED = Object.freeze({ data: undefined, error: null, isLoading: false });
export function toCanonical(args) {
    if (args === 'skip')
        return 'skip';
    const encoded = args === undefined ? {} : encodeValue(args);
    return canonicalJson(encoded);
}
export class QueryStore {
    client;
    path;
    args;
    result;
    serverSnapshot;
    listeners = new Set();
    unsubscribe = null;
    generation = 0;
    constructor(client, path, args) {
        this.client = client;
        this.path = path;
        this.args = args;
        const skip = args === 'skip';
        this.result = (skip ? SKIPPED : LOADING);
        this.serverSnapshot = this.result;
    }
    subscribe(listener) {
        this.listeners.add(listener);
        if (!this.unsubscribe && this.args !== 'skip') {
            this.generation += 1;
            const gen = this.generation;
            this.unsubscribe = this.client.watch(this.path, this.args, {
                onUpdate: (result) => {
                    if (gen !== this.generation)
                        return;
                    this.handleUpdate(result);
                },
                onError: (error) => {
                    if (gen !== this.generation)
                        return;
                    this.handleUpdate({ data: undefined, error, isLoading: false });
                },
            });
        }
        return () => {
            this.listeners.delete(listener);
            if (this.listeners.size === 0 && this.unsubscribe) {
                // Invalidate any callbacks from the watch that is about to be torn down.
                this.generation += 1;
                this.unsubscribe();
                this.unsubscribe = null;
            }
        };
    }
    handleUpdate(result) {
        if (this.isSameResult(result))
            return;
        this.result = result;
        for (const listener of this.listeners) {
            listener();
        }
    }
    isSameResult(next) {
        return (this.result.isLoading === next.isLoading &&
            this.result.error === next.error &&
            this.result.data === next.data);
    }
    getSnapshot() {
        return this.result;
    }
    getServerSnapshot() {
        return this.serverSnapshot;
    }
}
//# sourceMappingURL=store.js.map