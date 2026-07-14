import type { ClientLimits, ConnectionState, RealtimeTransport, Unsubscribe, WatchOptions } from './types.js';
export interface FetchRealtimeTransportOptions {
    baseUrl: string | URL;
    fetch?: typeof globalThis.fetch;
    getAuthToken?: () => string | Promise<string | undefined> | undefined;
    realtimePath?: string;
    maxReconnects?: number;
    initialReconnectDelayMs?: number;
    maxReconnectDelayMs?: number;
    limits?: ClientLimits;
    timeoutMs?: number;
}
export declare class FetchRealtimeTransport implements RealtimeTransport {
    readonly baseUrl: string;
    readonly fetchFn: typeof globalThis.fetch;
    readonly getAuthToken?: () => string | Promise<string | undefined> | undefined;
    readonly realtimePath: string;
    readonly maxReconnects: number;
    readonly initialReconnectDelayMs: number;
    readonly maxReconnectDelayMs: number;
    readonly maxFunctionArgsBytes: number;
    readonly maxReturnValueBytes: number;
    readonly maxRealtimeBodyBytes: number;
    readonly maxResponseBodyBytes: number;
    readonly maxSseEventDataLength: number;
    readonly maxSseLineLength: number;
    readonly timeoutMs: number;
    private readonly subscriptions;
    private closed;
    constructor(options: FetchRealtimeTransportOptions);
    computeSubscriptionId(path: string, canonicalArgs: string): Promise<string>;
    get connectionState(): ConnectionState;
    watch<Args, Return>(path: string, args: Args, options: WatchOptions<Return>): Unsubscribe;
    removeSubscription(subscriptionKey: string): void;
    refreshAuth(): void;
    close(): void;
}
//# sourceMappingURL=realtime.d.ts.map