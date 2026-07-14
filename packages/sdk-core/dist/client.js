import { encodeValue, decodeValue, canonicalJson, isJsonValue, isStructuredError, parseContentType, readBoundedText, DEFAULT_CONFIG, MAX_PATH_LENGTH, } from '@pbvex/protocol';
import { PBVexError } from './errors.js';
import { FetchRealtimeTransport } from './realtime.js';
const MAX_TIMEOUT_MS = 10 * 60 * 1000;
function resolveArgsAndOptions(rest, noArgs) {
    if (rest.length === 0)
        return { args: undefined, options: undefined };
    if (rest.length >= 2)
        return { args: rest[0], options: rest[1] };
    return noArgs
        ? { args: undefined, options: rest[0] }
        : { args: rest[0], options: undefined };
}
function isNoArgsRef(ref) {
    return ref !== null && typeof ref === 'object' &&
        ref.__noArgs === true;
}
function resolveBaseUrl(url, baseUrl) {
    const resolved = baseUrl ? new URL(baseUrl, url) : new URL(url);
    return resolved.toString();
}
function encodeCallArgs(args) {
    if (args === 'skip') {
        throw new Error('skip is reserved by adapter hooks and cannot be passed to core calls');
    }
    return args === undefined ? {} : encodeValue(args);
}
function abortError(reason) {
    if (reason instanceof Error)
        return reason;
    if (typeof DOMException !== 'undefined') {
        return new DOMException('Aborted', 'AbortError');
    }
    const error = new Error('AbortError');
    error.name = 'AbortError';
    return error;
}
function isValidTimeout(value) {
    return typeof value === 'number' && Number.isFinite(value) && value > 0 && value <= MAX_TIMEOUT_MS;
}
function validateTimeoutMs(value, fallback) {
    if (value === undefined)
        return fallback;
    if (!isValidTimeout(value)) {
        throw new TypeError(`timeoutMs must be a positive finite number <= ${MAX_TIMEOUT_MS}ms`);
    }
    return value;
}
function validateLimit(value, fallback, name) {
    if (value === undefined)
        return fallback;
    if (typeof value !== 'number' || !Number.isFinite(value) || value < 0 || !Number.isInteger(value)) {
        throw new TypeError(`${name} must be a non-negative integer`);
    }
    return value;
}
function byteLength(text) {
    return new TextEncoder().encode(text).length;
}
function isValidResultEnvelope(value) {
    return (typeof value === 'object' &&
        value !== null &&
        'result' in value &&
        isJsonValue(value.result));
}
export class Client {
    fetchFn;
    baseUrl;
    callUrl;
    realtimePath;
    maxFunctionArgsBytes;
    maxReturnValueBytes;
    timeoutMs;
    auth = null;
    transport;
    constructor(url, options = {}) {
        this.fetchFn = options.fetch ?? (typeof fetch !== 'undefined' ? fetch : undefined);
        if (!this.fetchFn) {
            throw new Error('No fetch implementation available');
        }
        this.baseUrl = resolveBaseUrl(url, options.baseUrl);
        this.callUrl = new URL('/api/pbvex/call', this.baseUrl).toString();
        this.realtimePath = options.realtimePath ?? '/api/pbvex/realtime';
        this.timeoutMs = validateTimeoutMs(options.timeoutMs, 30000);
        this.maxFunctionArgsBytes = validateLimit(options.limits?.maxFunctionArgsBytes, DEFAULT_CONFIG.maxFunctionArgsBytes, 'maxFunctionArgsBytes');
        this.maxReturnValueBytes = validateLimit(options.limits?.maxReturnValueBytes, DEFAULT_CONFIG.maxReturnValueBytes, 'maxReturnValueBytes');
        if (options.auth !== undefined) {
            this.auth = options.auth;
        }
        if (options.realtimeTransport) {
            this.transport = options.realtimeTransport;
        }
    }
    get connectionState() {
        return this.transport?.connectionState ?? 'disconnected';
    }
    setAuth(value) {
        this.auth = value;
        this.transport?.refreshAuth?.();
    }
    clearAuth() {
        this.auth = null;
        this.transport?.refreshAuth?.();
    }
    async resolveAuth(authOverride) {
        const source = authOverride ?? this.auth;
        if (source === null || source === undefined)
            return undefined;
        if (typeof source === 'string')
            return source;
        const token = await source();
        return token || undefined;
    }
    async callFunction(name, args, options) {
        const timeoutMs = validateTimeoutMs(options?.timeoutMs, this.timeoutMs);
        const argsJson = encodeCallArgs(args);
        const argsText = canonicalJson(argsJson);
        if (byteLength(argsText) > this.maxFunctionArgsBytes) {
            throw new Error(`Function args exceed ${this.maxFunctionArgsBytes} bytes`);
        }
        const body = {
            name,
            args: argsJson,
        };
        const bodyText = JSON.stringify(body);
        if (byteLength(bodyText) > this.maxFunctionArgsBytes + MAX_PATH_LENGTH + 100) {
            throw new Error(`Request body exceeds ${this.maxFunctionArgsBytes + MAX_PATH_LENGTH + 100} bytes`);
        }
        const controller = new AbortController();
        let timedOut = false;
        const timer = setTimeout(() => {
            timedOut = true;
            controller.abort();
        }, timeoutMs);
        const externalSignal = options?.signal ?? options?.abort;
        let externalAbortHandler;
        if (externalSignal) {
            if (externalSignal.aborted) {
                clearTimeout(timer);
                controller.abort();
                return Promise.reject(abortError(externalSignal.reason));
            }
            externalAbortHandler = () => controller.abort();
            externalSignal.addEventListener('abort', externalAbortHandler, { once: true });
        }
        let authAbortHandler;
        const authAbortPromise = new Promise((_, reject) => {
            authAbortHandler = () => reject(new Error('aborted'));
            controller.signal.addEventListener('abort', authAbortHandler, { once: true });
        });
        authAbortPromise.catch(() => { });
        try {
            const authPromise = this.resolveAuth(options?.auth);
            authPromise.catch(() => { });
            const token = await Promise.race([authPromise, authAbortPromise]);
            if (controller.signal.aborted) {
                throw new Error('aborted');
            }
            const headers = {
                'Content-Type': 'application/json',
            };
            if (token) {
                headers['Authorization'] = `Bearer ${token}`;
            }
            const response = await this.fetchFn(this.callUrl, {
                method: 'POST',
                headers,
                body: bodyText,
                signal: controller.signal,
            });
            const raw = await readBoundedText(response, this.maxReturnValueBytes + 4096, controller.signal);
            let json;
            try {
                json = JSON.parse(raw);
            }
            catch {
                json = undefined;
            }
            if (!response.ok) {
                if (isStructuredError(json)) {
                    throw new PBVexError(json);
                }
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }
            const contentType = response.headers.get('content-type') ?? '';
            const parsed = parseContentType(contentType);
            if (parsed?.mediaType !== 'application/json') {
                throw new Error(`Unexpected response content-type: ${contentType}`);
            }
            if (!isValidResultEnvelope(json)) {
                throw new Error('Malformed response: missing or invalid result');
            }
            return decodeValue(json.result);
        }
        catch (error) {
            if (error instanceof PBVexError) {
                throw error;
            }
            const externalAborted = externalSignal?.aborted ?? false;
            if (timedOut || (controller.signal.aborted && !externalAborted)) {
                throw new Error(`Request timeout after ${timeoutMs}ms`);
            }
            if (externalAborted) {
                throw abortError(externalSignal?.reason);
            }
            throw error;
        }
        finally {
            clearTimeout(timer);
            if (controller && authAbortHandler) {
                controller.signal.removeEventListener('abort', authAbortHandler);
            }
            if (externalSignal && externalAbortHandler) {
                externalSignal.removeEventListener('abort', externalAbortHandler);
            }
        }
    }
    query(ref, ...argsAndOptions) {
        const name = typeof ref === 'string' ? ref : ref._path;
        const { args, options } = typeof ref === 'string'
            ? { args: argsAndOptions[0], options: argsAndOptions[1] }
            : resolveArgsAndOptions(argsAndOptions, isNoArgsRef(ref));
        return this.callFunction(name, args, options);
    }
    mutation(ref, ...argsAndOptions) {
        const name = typeof ref === 'string' ? ref : ref._path;
        const { args, options } = typeof ref === 'string'
            ? { args: argsAndOptions[0], options: argsAndOptions[1] }
            : resolveArgsAndOptions(argsAndOptions, isNoArgsRef(ref));
        return this.callFunction(name, args, options);
    }
    action(ref, ...argsAndOptions) {
        const name = typeof ref === 'string' ? ref : ref._path;
        const { args, options } = typeof ref === 'string'
            ? { args: argsAndOptions[0], options: argsAndOptions[1] }
            : resolveArgsAndOptions(argsAndOptions, isNoArgsRef(ref));
        return this.callFunction(name, args, options);
    }
    getTransport() {
        if (!this.transport) {
            this.transport = new FetchRealtimeTransport({
                baseUrl: this.baseUrl,
                fetch: this.fetchFn,
                getAuthToken: () => this.resolveAuth(),
                realtimePath: this.realtimePath,
                timeoutMs: this.timeoutMs,
                limits: {
                    maxFunctionArgsBytes: this.maxFunctionArgsBytes,
                    maxReturnValueBytes: this.maxReturnValueBytes,
                },
            });
        }
        return this.transport;
    }
    watch(ref, ...argsAndOptions) {
        const name = typeof ref === 'string' ? ref : ref._path;
        const { args, options } = typeof ref === 'string'
            ? { args: argsAndOptions[0], options: argsAndOptions[1] }
            : resolveArgsAndOptions(argsAndOptions, isNoArgsRef(ref));
        if (args === 'skip') {
            throw new Error('skip is reserved by adapter hooks and cannot be passed to core calls');
        }
        return this.getTransport().watch(name, args, options ?? { onUpdate: () => { } });
    }
    close() {
        this.transport?.close();
        this.transport = undefined;
    }
}
export class PBVexClient extends Client {
}
//# sourceMappingURL=client.js.map