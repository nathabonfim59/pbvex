import { type ArgsAndOptions, type FunctionReference, type FunctionReturnType } from '@pbvex/protocol';
import type { AuthProvider, CallOptions, ClientOptions, ConnectionState, Unsubscribe, WatchOptions } from './types.js';
export declare class Client {
    private readonly fetchFn;
    private readonly baseUrl;
    private readonly callUrl;
    private readonly realtimePath;
    private readonly maxFunctionArgsBytes;
    private readonly maxReturnValueBytes;
    private timeoutMs;
    private auth;
    private transport;
    constructor(url: string | URL, options?: ClientOptions);
    get connectionState(): ConnectionState;
    setAuth(value: string | AuthProvider): void;
    clearAuth(): void;
    resolveAuth(authOverride?: string | AuthProvider): Promise<string | undefined>;
    private callFunction;
    query<Ref extends FunctionReference<'query', any, any, 'public'>>(ref: Ref, ...argsAndOptions: ArgsAndOptions<Ref, CallOptions>): Promise<FunctionReturnType<Ref>>;
    query<Args = any, Return = any>(name: string, args?: Args, options?: CallOptions): Promise<Return>;
    mutation<Ref extends FunctionReference<'mutation', any, any, 'public'>>(ref: Ref, ...argsAndOptions: ArgsAndOptions<Ref, CallOptions>): Promise<FunctionReturnType<Ref>>;
    mutation<Args = any, Return = any>(name: string, args?: Args, options?: CallOptions): Promise<Return>;
    action<Ref extends FunctionReference<'action', any, any, 'public'>>(ref: Ref, ...argsAndOptions: ArgsAndOptions<Ref, CallOptions>): Promise<FunctionReturnType<Ref>>;
    action<Args = any, Return = any>(name: string, args?: Args, options?: CallOptions): Promise<Return>;
    private getTransport;
    watch<Ref extends FunctionReference<'query', any, any, 'public'>>(ref: Ref, ...argsAndOptions: ArgsAndOptions<Ref, WatchOptions<FunctionReturnType<Ref>>>): Unsubscribe;
    watch<Return = any>(name: string, args?: unknown, options?: WatchOptions<Return>): Unsubscribe;
    close(): void;
}
export declare class PBVexClient extends Client {
}
//# sourceMappingURL=client.d.ts.map