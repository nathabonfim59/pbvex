import type { Client, QueryResult } from '@pbvex/sdk-core';
export declare function toCanonical(args: unknown): string;
export declare class QueryStore<Args, Return> {
    private readonly client;
    private readonly path;
    private readonly args;
    private result;
    private serverSnapshot;
    private listeners;
    private unsubscribe;
    private generation;
    constructor(client: Client, path: string, args: Args | 'skip' | undefined);
    subscribe(listener: () => void): () => void;
    private handleUpdate;
    private isSameResult;
    getSnapshot(): QueryResult<Return>;
    getServerSnapshot(): QueryResult<Return>;
}
//# sourceMappingURL=store.d.ts.map