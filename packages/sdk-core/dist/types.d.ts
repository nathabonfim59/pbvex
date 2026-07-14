import type { FunctionReference, FunctionArgs, FunctionReturnType, OptionalRestArgs, ArgsAndOptions, FunctionType, FunctionVisibility } from '@pbvex/protocol';
export type { FunctionType, FunctionVisibility };
export type { FunctionReference, FunctionArgs, FunctionReturnType, OptionalRestArgs, ArgsAndOptions, };
export type EmptyObject = Record<string, never>;
export type IsAny<T> = 0 extends (1 & T) ? true : false;
export type IsOptionalArgs<T> = IsAny<T> extends true ? false : [T] extends [undefined | void] ? true : T extends EmptyObject ? true : {} extends T ? true : false;
export type OptionalArgs<Args, Options> = IsOptionalArgs<Args> extends true ? [args?: Args, options?: Options] : [args: Args, options?: Options];
export type ArgsOf<T> = T extends FunctionReference<any, infer Args, any, any> ? Args : never;
export type ReturnOf<T> = T extends FunctionReference<any, any, infer Return, any> ? Return : never;
export type AuthProvider = () => string | Promise<string>;
export interface ClientLimits {
    maxFunctionArgsBytes?: number;
    maxReturnValueBytes?: number;
    maxUploadBytes?: number;
}
export interface CallOptions {
    timeoutMs?: number;
    auth?: string | AuthProvider;
    signal?: AbortSignal;
    /** @deprecated Use `signal`. */
    abort?: AbortSignal;
}
export interface ClientOptions {
    fetch?: typeof globalThis.fetch;
    baseUrl?: string;
    timeoutMs?: number;
    auth?: string | AuthProvider;
    realtimeTransport?: RealtimeTransport;
    realtimePath?: string;
    limits?: ClientLimits;
}
export type ConnectionState = 'connecting' | 'connected' | 'reconnecting' | 'disconnected';
export interface QueryResult<T> {
    data: T | undefined;
    error: Error | null;
    isLoading: boolean;
}
export interface WatchCallbacks<T> {
    onUpdate: (result: QueryResult<T>) => void;
    onError?: (error: Error) => void;
    onConnectionStateChange?: (state: ConnectionState) => void;
}
export interface WatchOptions<T> extends WatchCallbacks<T> {
    maxReconnects?: number;
    initialReconnectDelayMs?: number;
    maxReconnectDelayMs?: number;
}
export type Unsubscribe = () => void;
export interface RealtimeTransport {
    readonly connectionState: ConnectionState;
    refreshAuth?: () => void;
    watch<Args, Return>(path: string, args: Args, options: WatchOptions<Return>): Unsubscribe;
    close(): void;
}
//# sourceMappingURL=types.d.ts.map