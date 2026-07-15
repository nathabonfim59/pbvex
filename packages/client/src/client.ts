import {
  encodeValue,
  decodeValue,
  canonicalJson,
  isJsonValue,
  isStructuredError,
  parseContentType,
  readBoundedText,
  DEFAULT_CONFIG,
  MAX_PATH_LENGTH,
  type ArgsAndOptions,
  type FunctionReference,
  type FunctionReturnType,
  type JSONValue,
  type PbvexValue,
} from '@pbvex/protocol';
import { PBVexError } from './errors.js';
import type {
  AuthProvider,
  CallOptions,
  ClientOptions,
  ConnectionState,
  RealtimeTransport,
  Unsubscribe,
  WatchOptions,
} from './types.js';
import { FetchRealtimeTransport } from './realtime.js';
import {
  AuthApiError,
  AuthClient,
  AuthStore,
  LocalAuthStore,
  type AuthRecord,
  type AuthRequestOptions,
  type AuthResponse,
  type OAuth2Options,
} from './auth.js';

const MAX_TIMEOUT_MS = 10 * 60 * 1000;

function resolveArgsAndOptions<O>(
  rest: readonly unknown[],
  noArgs: boolean,
): { args: unknown; options: O | undefined } {
  if (rest.length === 0) return { args: undefined, options: undefined };
  if (rest.length >= 2) return { args: rest[0], options: rest[1] as O };
  return noArgs
    ? { args: undefined, options: rest[0] as O }
    : { args: rest[0], options: undefined };
}

function isNoArgsRef(ref: unknown): boolean {
  return ref !== null && typeof ref === 'object' &&
    (ref as Record<string, unknown>).__noArgs === true;
}

function resolveBaseUrl(url: string | URL, baseUrl: string | undefined): string {
  const resolved = baseUrl ? new URL(baseUrl, url) : new URL(url);
  return resolved.toString();
}

function encodeCallArgs(args: unknown): JSONValue {
  if (args === 'skip') {
    throw new Error('skip is reserved by adapter hooks and cannot be passed to core calls');
  }
  return args === undefined ? ({} as JSONValue) : encodeValue(args as PbvexValue);
}

function abortError(reason?: unknown): Error {
  if (reason instanceof Error) return reason;
  if (typeof DOMException !== 'undefined') {
    return new DOMException('Aborted', 'AbortError');
  }
  const error = new Error('AbortError');
  error.name = 'AbortError';
  return error;
}

function isValidTimeout(value: number | undefined): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 && value <= MAX_TIMEOUT_MS;
}

function validateTimeoutMs(value: number | undefined, fallback: number): number {
  if (value === undefined) return fallback;
  if (!isValidTimeout(value)) {
    throw new TypeError(`timeoutMs must be a positive finite number <= ${MAX_TIMEOUT_MS}ms`);
  }
  return value;
}

function validateLimit(value: number | undefined, fallback: number, name: string): number {
  if (value === undefined) return fallback;
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0 || !Number.isInteger(value)) {
    throw new TypeError(`${name} must be a non-negative integer`);
  }
  return value;
}

function openAuthPopup(url?: string): Window | null {
  if (typeof window === 'undefined' || typeof window.open !== 'function') {
    throw new Error('OAuth2 popup authentication requires a browser; provide urlCallback or use authWithOAuth2Code');
  }
  const width = Math.min(1024, window.innerWidth);
  const height = Math.min(768, window.innerHeight);
  const left = window.innerWidth / 2 - width / 2;
  const top = window.innerHeight / 2 - height / 2;
  return window.open(
    url,
    'pbvex_oauth2',
    `width=${width},height=${height},top=${top},left=${left},resizable,menubar=no`,
  );
}

function byteLength(text: string): number {
  return new TextEncoder().encode(text).length;
}

function isValidResultEnvelope(value: unknown): value is { result: JSONValue } {
  return (
    typeof value === 'object' &&
    value !== null &&
    'result' in value &&
    isJsonValue((value as Record<string, unknown>).result)
  );
}

export class Client {
  private readonly fetchFn: typeof globalThis.fetch;
  private readonly baseUrl: string;
  private readonly callUrl: string;
  private readonly realtimePath: string;
  private readonly maxFunctionArgsBytes: number;
  private readonly maxReturnValueBytes: number;
  private timeoutMs: number;
  private authSource: string | AuthProvider | null;
  private transport: RealtimeTransport | undefined;
  readonly authStore: AuthStore;
  readonly auth: AuthClient;

  constructor(url: string | URL, options: ClientOptions = {}) {
    this.fetchFn = options.fetch ?? (typeof fetch !== 'undefined' ? fetch : undefined as never);
    if (!this.fetchFn) {
      throw new Error('No fetch implementation available');
    }
    this.baseUrl = resolveBaseUrl(url, options.baseUrl);
    this.callUrl = new URL('/api/pbvex/call', this.baseUrl).toString();
    this.realtimePath = options.realtimePath ?? '/api/pbvex/realtime';
    this.timeoutMs = validateTimeoutMs(options.timeoutMs, 30000);
    this.maxFunctionArgsBytes = validateLimit(options.limits?.maxFunctionArgsBytes, DEFAULT_CONFIG.maxFunctionArgsBytes, 'maxFunctionArgsBytes');
    this.maxReturnValueBytes = validateLimit(options.limits?.maxReturnValueBytes, DEFAULT_CONFIG.maxReturnValueBytes, 'maxReturnValueBytes');
    this.authStore = options.authStore ?? new LocalAuthStore();
    this.authSource = options.auth ?? (() => this.authStore.token);
    this.authStore.onChange(() => this.transport?.refreshAuth?.());
    this.auth = new AuthClient(
      (path, init) => this.authRequest(path, init),
      this.authStore,
      (collection, oauthOptions) => this.authWithOAuth2(collection, oauthOptions),
    );
    if (options.realtimeTransport) {
      this.transport = options.realtimeTransport;
    }
  }

  get connectionState(): ConnectionState {
    return this.transport?.connectionState ?? 'disconnected';
  }

  setAuth(value: string | AuthProvider): void {
    this.authSource = value;
    this.transport?.refreshAuth?.();
  }

  clearAuth(): void {
    this.authSource = null;
    this.transport?.refreshAuth?.();
  }

  async resolveAuth(authOverride?: string | AuthProvider): Promise<string | undefined> {
    const source = authOverride ?? this.authSource;
    if (source === null || source === undefined) return undefined;
    if (typeof source === 'string') return source;
    const token = await source();
    return token || undefined;
  }

  private async authRequest<T>(
    path: string,
    init: {
      method?: string;
      body?: Record<string, unknown>;
      options?: AuthRequestOptions;
      auth?: boolean;
    } = {},
  ): Promise<T> {
    const url = new URL(path, this.baseUrl);
    for (const [key, value] of Object.entries(init.options?.query ?? {})) {
      if (value !== undefined) url.searchParams.set(key, String(value));
    }

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    const externalSignal = init.options?.signal;
    const onAbort = () => controller.abort(externalSignal?.reason);
    if (externalSignal?.aborted) onAbort();
    else externalSignal?.addEventListener('abort', onAbort, { once: true });

    try {
      const headers: Record<string, string> = { ...init.options?.headers };
      let body: string | undefined;
      if (init.body !== undefined) {
        headers['Content-Type'] ??= 'application/json';
        body = JSON.stringify(init.body);
      }
      if (init.auth !== false) {
        const token = await this.resolveAuth();
        if (token) headers.Authorization ??= `Bearer ${token}`;
      }

      const response = await this.fetchFn(url, {
        method: init.method ?? 'GET',
        headers,
        body,
        signal: controller.signal,
      });
      const raw = await readBoundedText(response, this.maxReturnValueBytes + 4096, controller.signal);
      let json: unknown;
      if (raw) {
        try { json = JSON.parse(raw); } catch { json = undefined; }
      }
      if (!response.ok) {
        const errorBody = json && typeof json === 'object'
          ? json as Record<string, unknown>
          : { status: response.status, message: response.statusText };
        throw new AuthApiError(response.status, errorBody, url.toString());
      }
      return json as T;
    } catch (error) {
      if (externalSignal?.aborted) throw abortError(externalSignal.reason);
      if (controller.signal.aborted) throw new Error(`Request timeout after ${this.timeoutMs}ms`);
      throw error;
    } finally {
      clearTimeout(timer);
      externalSignal?.removeEventListener('abort', onAbort);
    }
  }

  private async authWithOAuth2<T extends AuthRecord>(
    collection: string,
    options: OAuth2Options,
  ): Promise<AuthResponse<T>> {
    let popup: Window | null = null;
    if (!options.urlCallback) popup = openAuthPopup();

    const collectionAuth = this.auth.collection<T>(collection);
    try {
      const methods = await collectionAuth.listAuthMethods(options);
      const provider = methods.oauth2.providers.find((candidate) => candidate.name === options.provider);
      if (!provider) throw new Error(`Missing or invalid OAuth2 provider "${options.provider}"`);
      if (typeof EventSource === 'undefined') {
        throw new Error('OAuth2 popup authentication requires EventSource; use authWithOAuth2Code in this environment');
      }

      const redirectURL = new URL('/api/oauth2-redirect', this.baseUrl).toString();
      return await new Promise<AuthResponse<T>>((resolve, reject) => {
        const events = new EventSource(new URL('/api/realtime', this.baseUrl));
        let clientId = '';
        let settled = false;
        const cleanup = () => {
          events.close();
          popup?.close();
          options.signal?.removeEventListener('abort', onAbort);
        };
        const finish = (fn: () => void) => {
          if (settled) return;
          settled = true;
          cleanup();
          fn();
        };
        const onAbort = () => finish(() => reject(abortError(options.signal?.reason)));
        options.signal?.addEventListener('abort', onAbort, { once: true });

        events.onerror = () => finish(() => reject(new Error('OAuth2 realtime connection interrupted')));
        events.addEventListener('PB_CONNECT', async (event) => {
          try {
            const data = JSON.parse((event as MessageEvent<string>).data) as { clientId?: string };
            if (!data.clientId) throw new Error('OAuth2 realtime connection did not return a client id');
            clientId = data.clientId;
            await this.authRequest('/api/realtime', {
              method: 'POST', body: { clientId, subscriptions: ['@oauth2'] }, options,
            });
            const authURL = new URL(provider.authURL + redirectURL);
            authURL.searchParams.set('state', clientId);
            if (options.scopes?.length) authURL.searchParams.set('scope', options.scopes.join(' '));
            if (options.urlCallback) await options.urlCallback(authURL.toString());
            else if (popup) popup.location.href = authURL.toString();
            else popup = openAuthPopup(authURL.toString());
          } catch (error) {
            finish(() => reject(error));
          }
        });
        events.addEventListener('@oauth2', async (event) => {
          try {
            const data = JSON.parse((event as MessageEvent<string>).data) as {
              state?: string; code?: string; error?: string;
            };
            if (!data.state || data.state !== clientId) throw new Error('OAuth2 state parameters do not match');
            if (data.error || !data.code) throw new Error(`OAuth2 redirect error or missing code: ${data.error ?? ''}`);
            const result = await collectionAuth.authWithOAuth2Code(
              provider.name,
              data.code,
              provider.codeVerifier,
              redirectURL,
              options.createData,
              options,
            );
            finish(() => resolve(result));
          } catch (error) {
            finish(() => reject(error));
          }
        });
      });
    } catch (error) {
      popup?.close();
      throw error;
    }
  }

  private async callFunction<Return>(
    name: string,
    args: unknown,
    options?: CallOptions,
  ): Promise<Return> {
    const timeoutMs = validateTimeoutMs(options?.timeoutMs, this.timeoutMs);
    const argsJson = encodeCallArgs(args);
    const argsText = canonicalJson(argsJson);
    if (byteLength(argsText) > this.maxFunctionArgsBytes) {
      throw new Error(`Function args exceed ${this.maxFunctionArgsBytes} bytes`);
    }
    const body: JSONValue = {
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
    let externalAbortHandler: (() => void) | undefined;
    if (externalSignal) {
      if (externalSignal.aborted) {
        clearTimeout(timer);
        controller.abort();
        return Promise.reject(abortError(externalSignal.reason));
      }
      externalAbortHandler = () => controller.abort();
      externalSignal.addEventListener('abort', externalAbortHandler, { once: true });
    }

    let authAbortHandler: (() => void) | undefined;
    const authAbortPromise = new Promise<never>((_, reject) => {
      authAbortHandler = () => reject(new Error('aborted'));
      controller.signal.addEventListener('abort', authAbortHandler, { once: true });
    });
    authAbortPromise.catch(() => {});

    try {
      const authPromise = this.resolveAuth(options?.auth);
      authPromise.catch(() => {});
      const token = await Promise.race([authPromise, authAbortPromise]);
      if (controller.signal.aborted) {
        throw new Error('aborted');
      }

      const headers: Record<string, string> = {
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

      let json: unknown;
      try {
        json = JSON.parse(raw);
      } catch {
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

      return decodeValue(json.result) as Return;
    } catch (error) {
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
    } finally {
      clearTimeout(timer);
      if (controller && authAbortHandler) {
        controller.signal.removeEventListener('abort', authAbortHandler);
      }
      if (externalSignal && externalAbortHandler) {
        externalSignal.removeEventListener('abort', externalAbortHandler);
      }
    }
  }

  query<Ref extends FunctionReference<'query', any, any, 'public'>>(
    ref: Ref,
    ...argsAndOptions: ArgsAndOptions<Ref, CallOptions>
  ): Promise<FunctionReturnType<Ref>>;
  query<Args = any, Return = any>(
    name: string,
    args?: Args,
    options?: CallOptions,
  ): Promise<Return>;
  query(ref: any, ...argsAndOptions: any[]): Promise<any> {
    const name = typeof ref === 'string' ? ref : (ref as FunctionReference<any, any, any, any>)._path;
    const { args, options } = typeof ref === 'string'
      ? { args: argsAndOptions[0], options: argsAndOptions[1] }
      : resolveArgsAndOptions<CallOptions>(argsAndOptions, isNoArgsRef(ref));
    return this.callFunction(name, args, options);
  }

  mutation<Ref extends FunctionReference<'mutation', any, any, 'public'>>(
    ref: Ref,
    ...argsAndOptions: ArgsAndOptions<Ref, CallOptions>
  ): Promise<FunctionReturnType<Ref>>;
  mutation<Args = any, Return = any>(
    name: string,
    args?: Args,
    options?: CallOptions,
  ): Promise<Return>;
  mutation(ref: any, ...argsAndOptions: any[]): Promise<any> {
    const name = typeof ref === 'string' ? ref : (ref as FunctionReference<any, any, any, any>)._path;
    const { args, options } = typeof ref === 'string'
      ? { args: argsAndOptions[0], options: argsAndOptions[1] }
      : resolveArgsAndOptions<CallOptions>(argsAndOptions, isNoArgsRef(ref));
    return this.callFunction(name, args, options);
  }

  action<Ref extends FunctionReference<'action', any, any, 'public'>>(
    ref: Ref,
    ...argsAndOptions: ArgsAndOptions<Ref, CallOptions>
  ): Promise<FunctionReturnType<Ref>>;
  action<Args = any, Return = any>(
    name: string,
    args?: Args,
    options?: CallOptions,
  ): Promise<Return>;
  action(ref: any, ...argsAndOptions: any[]): Promise<any> {
    const name = typeof ref === 'string' ? ref : (ref as FunctionReference<any, any, any, any>)._path;
    const { args, options } = typeof ref === 'string'
      ? { args: argsAndOptions[0], options: argsAndOptions[1] }
      : resolveArgsAndOptions<CallOptions>(argsAndOptions, isNoArgsRef(ref));
    return this.callFunction(name, args, options);
  }

  private getTransport(): RealtimeTransport {
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

  watch<Ref extends FunctionReference<'query', any, any, 'public'>>(
    ref: Ref,
    ...argsAndOptions: ArgsAndOptions<Ref, WatchOptions<FunctionReturnType<Ref>>>
  ): Unsubscribe;
  watch<Return = any>(
    name: string,
    args?: unknown,
    options?: WatchOptions<Return>,
  ): Unsubscribe;
  watch(ref: any, ...argsAndOptions: any[]): Unsubscribe {
    const name = typeof ref === 'string' ? ref : (ref as FunctionReference<any, any, any, any>)._path;
    const { args, options } = typeof ref === 'string'
      ? { args: argsAndOptions[0], options: argsAndOptions[1] }
      : resolveArgsAndOptions<WatchOptions<unknown>>(argsAndOptions, isNoArgsRef(ref));
    if (args === 'skip') {
      throw new Error('skip is reserved by adapter hooks and cannot be passed to core calls');
    }
    return this.getTransport().watch(name, args, options ?? { onUpdate: () => {} });
  }

  close(): void {
    this.transport?.close();
    this.transport = undefined;
  }
}

export class PBVexClient extends Client {}
