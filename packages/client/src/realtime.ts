import {
  decodeValue,
  canonicalJson,
  encodeValue,
  hashSha256,
  isSseEnvelope,
  isStructuredError,
  parseContentType,
  readBoundedText,
  DEFAULT_CONFIG,
  MAX_PATH_LENGTH,
  type JSONValue,
  type PbvexValue,
} from '@pbvex/protocol';
import { PBVexError } from './errors.js';
import type {
  ClientLimits,
  ConnectionState,
  QueryResult,
  RealtimeTransport,
  Unsubscribe,
  WatchOptions,
} from './types.js';

const MAX_SSE_DECODE_SLICE = 8192;

const MAX_RECONNECTS = 100;
const MAX_RECONNECT_DELAY_MS = 24 * 60 * 60 * 1000;
const MAX_INITIAL_RECONNECT_DELAY_MS = 60 * 1000;

function isValidResponseBody(body: unknown): body is { getReader(): ReadableStreamDefaultReader<Uint8Array> } {
  return (
    body !== null &&
    typeof body === 'object' &&
    typeof (body as { getReader?: unknown }).getReader === 'function'
  );
}

function defaultEncodeArgs(args: unknown): JSONValue {
  if (args === 'skip') {
    throw new Error('skip is reserved by adapter hooks and cannot be passed to realtime core');
  }
  return args === undefined ? ({} as JSONValue) : (encodeValue(args as PbvexValue) as JSONValue);
}

function toError(value: unknown): Error {
  if (value instanceof Error) return value;
  if (typeof value === 'string') return new Error(value);
  return new Error(String(value));
}

function byteLength(text: string): number {
  return new TextEncoder().encode(text).length;
}

interface SseParserCallbacks {
  onData: (data: string) => void;
  onError: (error: Error) => void;
}

class SseParser {
  private decoder = new TextDecoder('utf-8', { fatal: true });
  private buffer = '';
  private bufferBytes = 0;
  private pendingData: string[] = [];
  private pendingDataBytes = 0;
  private discardEvent = false;
  private discardErrorReported = false;
  private callbacks: SseParserCallbacks | undefined;

  // The client-configured baseline limits; restored on reset() so each
  // connection re-negotiates from the safety ceiling.
  private readonly baselineMaxLineLength: number;
  private readonly baselineMaxEventDataLength: number;
  private maxLineLength: number;
  private maxEventDataLength: number;

  constructor(maxLineLength: number, maxEventDataLength: number) {
    this.baselineMaxLineLength = maxLineLength;
    this.baselineMaxEventDataLength = maxEventDataLength;
    this.maxLineLength = maxLineLength;
    this.maxEventDataLength = maxEventDataLength;
  }

  reset(): void {
    this.decoder = new TextDecoder('utf-8', { fatal: true });
    this.buffer = '';
    this.bufferBytes = 0;
    this.pendingData = [];
    this.pendingDataBytes = 0;
    this.discardEvent = false;
    this.discardErrorReported = false;
    // Restore the client ceiling so a fresh connection is not stuck at a
    // limit tightened by a previous connection's negotiation.
    this.maxLineLength = this.baselineMaxLineLength;
    this.maxEventDataLength = this.baselineMaxEventDataLength;
  }

  /**
   * Tighten the event-data limit to a server-negotiated value. The parser is
   * constructed with the client's safety ceiling, so only tightening (never
   * loosening) keeps the effective limit at min(server, ceiling).
   */
  negotiateMaxEventDataLength(limit: number): void {
    if (!Number.isFinite(limit) || limit <= 0) return;
    const floored = Math.floor(limit);
    if (floored < this.maxEventDataLength) {
      this.maxEventDataLength = floored;
      this.maxLineLength = floored + 6;
    }
  }

  feed(chunk: Uint8Array | undefined, done: boolean, callbacks: SseParserCallbacks): void {
    this.callbacks = callbacks;

    if (chunk) {
      for (let offset = 0; offset < chunk.length; offset += MAX_SSE_DECODE_SLICE) {
        const slice = chunk.subarray(offset, offset + MAX_SSE_DECODE_SLICE);
        let text: string;
        try {
          text = this.decoder.decode(slice, { stream: true });
        } catch {
          throw new Error('Invalid UTF-8 in SSE stream');
        }
        this.buffer += text;
        this.bufferBytes += byteLength(text);
        this.processBuffer();
        if (this.discardEvent && this.buffer.length > this.maxLineLength) {
          this.buffer = this.buffer.slice(-this.maxLineLength);
          this.bufferBytes = byteLength(this.buffer);
        } else if (!this.discardEvent && this.bufferBytes > this.maxLineLength && this.buffer.indexOf('\n') === -1) {
          this.beginDiscard(new Error('SSE line exceeded maximum length'));
        }
      }
    }

    if (done) {
      this.finalize();
    }
  }

  private processBuffer(): void {
    for (;;) {
      const idx = this.buffer.indexOf('\n');
      if (idx === -1) break;
      let line = this.buffer.slice(0, idx);
      this.buffer = this.buffer.slice(idx + 1);
      this.bufferBytes = byteLength(this.buffer);
      if (line.endsWith('\r')) {
        line = line.slice(0, -1);
      }

      if (this.discardEvent) {
        if (line.length === 0) {
          this.discardEvent = false;
          this.discardErrorReported = false;
        }
        continue;
      }

      this.processLine(line);
    }
  }

  private processLine(line: string): void {
    if (line.length === 0) {
      this.dispatch();
      return;
    }

    if (line.startsWith(':')) {
      return;
    }

    const lineBytes = byteLength(line);
    if (lineBytes > this.maxLineLength) {
      this.beginDiscard(new Error('SSE line exceeded maximum length'));
      return;
    }

    const colonIndex = line.indexOf(':');
    const field = colonIndex === -1 ? line : line.slice(0, colonIndex);
    let value = colonIndex === -1 ? '' : line.slice(colonIndex + 1);
    if (value.startsWith(' ')) {
      value = value.slice(1);
    }

    if (field !== 'data') {
      return;
    }

    const valueBytes = byteLength(value);
    const addedBytes = valueBytes + (this.pendingData.length > 0 ? 1 : 0);
    if (
      this.pendingDataBytes + addedBytes > this.maxEventDataLength
    ) {
      this.beginDiscard(new Error('SSE event data exceeded maximum length'));
      return;
    }

    this.pendingData.push(value);
    this.pendingDataBytes += addedBytes;
  }

  private dispatch(): void {
    if (this.pendingData.length === 0) return;
    const data = this.pendingData.join('\n');
    this.pendingData = [];
    this.pendingDataBytes = 0;
    this.callbacks?.onData(data);
  }

  private beginDiscard(error: Error): void {
    this.discardEvent = true;
    this.pendingData = [];
    this.pendingDataBytes = 0;
    if (!this.discardErrorReported) {
      this.discardErrorReported = true;
      this.callbacks?.onError(error);
    }
    // Keep a bounded tail so a blank-line delimiter split across chunks can still be detected.
    if (this.buffer.length > this.maxLineLength) {
      this.buffer = this.buffer.slice(-this.maxLineLength);
      this.bufferBytes = byteLength(this.buffer);
    }
  }

  private finalize(): void {
    // A connection that ends mid-line or mid-event must not carry state to the next connection.
    // Flushing the decoder also rejects a truncated UTF-8 sequence at EOF.
    try {
      this.decoder.decode(undefined, { stream: false });
    } catch {
      this.reset();
      throw new Error('Invalid UTF-8 in SSE stream');
    }
    this.reset();
  }
}

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

interface Watcher {
  id: number;
  options: WatchOptions<unknown>;
}

const MAX_TIMEOUT_MS = 10 * 60 * 1000;

function abortError(reason?: unknown): Error {
  if (reason instanceof Error) return reason;
  if (typeof DOMException !== 'undefined') {
    return new DOMException('Aborted', 'AbortError');
  }
  const error = new Error('AbortError');
  error.name = 'AbortError';
  return error;
}

function validateReconnectValue(value: number | undefined, fallback: number, name: string, max: number): number {
  if (value === undefined) return fallback;
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0 || value > max || !Number.isInteger(value)) {
    throw new TypeError(`${name} must be a non-negative integer <= ${max}`);
  }
  return value;
}

function validateDelay(value: number | undefined, fallback: number, name: string, max: number): number {
  if (value === undefined) return fallback;
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0 || value > max || !Number.isInteger(value)) {
    throw new TypeError(`${name} must be a positive integer <= ${max}ms`);
  }
  return value;
}

function validateTimeoutMs(value: number | undefined, fallback: number): number {
  if (value === undefined) return fallback;
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0 || value > MAX_TIMEOUT_MS || !Number.isInteger(value)) {
    throw new TypeError(`timeoutMs must be a positive integer <= ${MAX_TIMEOUT_MS}ms`);
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

function validateWatchOptions(options: WatchOptions<unknown>): void {
  if (options.maxReconnects !== undefined) {
    validateReconnectValue(options.maxReconnects, options.maxReconnects, 'maxReconnects', MAX_RECONNECTS);
  }
  if (options.initialReconnectDelayMs !== undefined) {
    validateDelay(options.initialReconnectDelayMs, options.initialReconnectDelayMs, 'initialReconnectDelayMs', MAX_INITIAL_RECONNECT_DELAY_MS);
  }
  if (options.maxReconnectDelayMs !== undefined) {
    validateDelay(options.maxReconnectDelayMs, options.maxReconnectDelayMs, 'maxReconnectDelayMs', MAX_RECONNECT_DELAY_MS);
  }
}

function safeIterate<T>(items: T[], fn: (item: T) => void): void {
  for (const item of items) {
    try {
      fn(item);
    } catch {
      // Isolated callbacks must not break the stream or starve other watchers.
    }
  }
}

class Subscription {
  private static nextWatcherId = 1;

  private readonly transport: FetchRealtimeTransport;
  readonly subscriptionKey: string;
  readonly path: string;
  readonly args: unknown;
  private readonly encodedArgs: JSONValue;
  private readonly canonicalArgs: string;
  private options: WatchOptions<unknown>;
  private watchers: Watcher[] = [];
  private state: ConnectionState = 'connecting';
  private abortController: AbortController | undefined;
  private reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
  private reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  private reconnectAttempt = 0;
  private parser: SseParser;
  private isDisposed = false;
  private latestResult: QueryResult<unknown> = { data: undefined, error: null, isLoading: true };
  private connectionId = 0;
  id = '';

  constructor(
    transport: FetchRealtimeTransport,
    subscriptionKey: string,
    path: string,
    args: unknown,
    encodedArgs: JSONValue,
    canonicalArgs: string,
    options: WatchOptions<unknown>,
  ) {
    validateWatchOptions(options);
    this.transport = transport;
    this.subscriptionKey = subscriptionKey;
    this.path = path;
    this.args = args;
    this.encodedArgs = encodedArgs;
    this.canonicalArgs = canonicalArgs;
    this.options = options;
    this.parser = new SseParser(transport.maxSseLineLength, transport.maxSseEventDataLength);
    this.start();
  }

  addWatcher<T>(options: WatchOptions<T>): Unsubscribe {
    // Reconnect options are owned by the first watcher that creates the subscription.
    // Subsequent watchers on the same subscription share the connection and are not
    // allowed to override the connection-level policy.
    const watcher: Watcher = { id: Subscription.nextWatcherId++, options: options as WatchOptions<unknown> };
    this.watchers.push(watcher);
    this.notifyConnectionStateChange(watcher);
    this.notifyUpdate(watcher, this.latestResult);
    return () => this.removeWatcher(watcher.id);
  }

  private removeWatcher(watcherId: number): void {
    this.watchers = this.watchers.filter((w) => w.id !== watcherId);
    if (this.watchers.length === 0) {
      this.dispose();
    }
  }

  private notifyConnectionStateChange(watcher: Watcher): void {
    try {
      watcher.options.onConnectionStateChange?.(this.state);
    } catch {
      // callback isolation
    }
  }

  private notifyUpdate(watcher: Watcher, result: QueryResult<unknown>): void {
    try {
      watcher.options.onUpdate(result);
    } catch {
      // callback isolation
    }
  }

  private notifyError(watcher: Watcher, error: Error): void {
    try {
      watcher.options.onError?.(error);
    } catch {
      // callback isolation
    }
  }

  private notifyStateChange(): void {
    safeIterate(this.watchers, (watcher) => this.notifyConnectionStateChange(watcher));
  }

  private updateAll(result: QueryResult<unknown>): void {
    this.latestResult = result;
    safeIterate(this.watchers, (watcher) => this.notifyUpdate(watcher, result));
  }

  private reportError(error: Error): void {
    safeIterate(this.watchers, (watcher) => this.notifyError(watcher, error));
  }

  private get maxReconnects(): number {
    return this.options.maxReconnects !== undefined
      ? this.options.maxReconnects
      : this.transport.maxReconnects;
  }

  private get initialReconnectDelayMs(): number {
    return this.options.initialReconnectDelayMs !== undefined
      ? this.options.initialReconnectDelayMs
      : this.transport.initialReconnectDelayMs;
  }

  private get maxReconnectDelayMs(): number {
    return this.options.maxReconnectDelayMs !== undefined
      ? this.options.maxReconnectDelayMs
      : this.transport.maxReconnectDelayMs;
  }

  private setState(newState: ConnectionState): void {
    if (this.state === newState) return;
    this.state = newState;
    this.notifyStateChange();
  }

  getState(): ConnectionState {
    return this.state;
  }

  private start(): void {
    if (this.isDisposed) return;
    this.setState('connecting');
    this.updateAll({ data: undefined, error: null, isLoading: true });
    this.openConnection();
  }

  refreshAuth(): void {
    if (this.isDisposed) return;
    this.clearReconnectTimer();
    this.reconnectAttempt = 0;
    this.reader?.cancel().catch(() => {});
    this.reader = undefined;
    this.abortController?.abort();
    this.openConnection();
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = undefined;
    }
  }

  private async openConnection(): Promise<void> {
    if (this.isDisposed) return;
    const openConnectionId = ++this.connectionId;
    this.setState('connecting');
    this.clearReconnectTimer();
    this.reader?.cancel().catch(() => {});
    this.reader = undefined;
    this.abortController?.abort();
    this.abortController = new AbortController();
    this.parser.reset();
    const currentController = this.abortController;
    // Generation-local reader reference. Cleanup in the catch uses this, never
    // the shared this.reader, so a delayed failure from a stale generation
    // cannot cancel a newer generation's live connection.
    let currentReader: ReadableStreamDefaultReader<Uint8Array> | undefined;
    // A single deadline covers auth, fetch headers, and reader acquisition. It
    // is cleared once the SSE read loop begins so established long-lived
    // streams are never aborted by the timer.
    let timedOut = false;
    let establishTimer: ReturnType<typeof setTimeout> | undefined;
    const startEstablishTimer = (): void => {
      establishTimer = setTimeout(() => {
        timedOut = true;
        currentController.abort();
      }, this.transport.timeoutMs);
    };
    const clearEstablishTimer = (): void => {
      if (establishTimer) {
        clearTimeout(establishTimer);
        establishTimer = undefined;
      }
    };

    try {
      startEstablishTimer();

      // Race getAuthToken against close/refresh abort; the establish timer
      // aborts the controller (and thus auth) if the deadline elapses.
      let token: string | undefined;
      let authAbortHandler: (() => void) | undefined;
      try {
        const authPromise = Promise.resolve(this.transport.getAuthToken ? this.transport.getAuthToken() : undefined);
        const authAbortPromise = new Promise<never>((_, reject) => {
          authAbortHandler = () => reject(abortError(currentController.signal.reason));
          currentController.signal.addEventListener('abort', authAbortHandler, { once: true });
        });
        authAbortPromise.catch(() => {});
        token = await Promise.race([authPromise, authAbortPromise]);
      } finally {
        if (authAbortHandler) {
          currentController.signal.removeEventListener('abort', authAbortHandler);
        }
      }
      if (this.isDisposed || this.connectionId !== openConnectionId) return;

      this.id = await this.transport.computeSubscriptionId(this.path, this.canonicalArgs);
      if (this.isDisposed || this.connectionId !== openConnectionId) return;

      const body: JSONValue = {
        id: this.id,
        path: this.path,
        args: this.encodedArgs,
      };
      const bodyText = canonicalJson(body);
      const bodyBytes = byteLength(bodyText);
      if (bodyBytes > this.transport.maxRealtimeBodyBytes) {
        throw new Error(`Realtime subscription request body exceeds ${this.transport.maxRealtimeBodyBytes} bytes`);
      }

      const url = new URL(this.transport.realtimePath, this.transport.baseUrl);

      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
        'Cache-Control': 'no-cache',
      };
      if (token) {
        headers['Authorization'] = `Bearer ${token}`;
      }

      const response = await this.transport.fetchFn(url.toString(), {
        method: 'POST',
        headers,
        body: bodyText,
        signal: currentController.signal,
      });
      if (this.isDisposed || this.connectionId !== openConnectionId) return;

      if (!response.ok) {
        const raw = await readBoundedText(response, this.transport.maxResponseBodyBytes, currentController.signal);
        if (this.isDisposed || this.connectionId !== openConnectionId) return;
        let json: unknown;
        try {
          json = JSON.parse(raw);
        } catch {
          json = undefined;
        }
        if (isStructuredError(json)) {
          throw new PBVexError(json);
        }
        throw new Error(`Realtime connection failed: HTTP ${response.status}`);
      }

      const contentType = response.headers.get('content-type') ?? '';
      const parsed = parseContentType(contentType);
      if (parsed?.mediaType !== 'text/event-stream') {
        throw new Error(`Realtime connection failed: unexpected content-type ${contentType}`);
      }
      if (this.isDisposed || this.connectionId !== openConnectionId) return;

      if (!isValidResponseBody(response.body)) {
        throw new Error('Realtime response does not have a readable body');
      }

      this.reader = response.body.getReader();
      currentReader = this.reader;

      // Establishment is complete: clear the deadline so long-lived SSE reads
      // from an established stream are never aborted by the timer.
      clearEstablishTimer();

      this.setState('connected');

      while (!this.isDisposed && this.connectionId === openConnectionId) {
        const { done, value } = await currentReader.read();
        if (this.isDisposed || this.connectionId !== openConnectionId) return;
        if (done) {
          this.processChunk(undefined, openConnectionId, true);
          break;
        }
        if (value) {
          this.processChunk(value, openConnectionId, false);
        }
      }

      if (this.isDisposed || this.connectionId !== openConnectionId) return;
      throw new Error('Realtime stream closed');
    } catch (error) {
      // Clean up generation-local resources. currentReader and currentController
      // belong only to this generation, so cancelling/aborting them cannot
      // affect a newer generation's connection.
      currentReader?.cancel().catch(() => {});
      currentController.abort();
      // Only mutate shared state if this generation is still current.
      if (this.isDisposed || this.connectionId !== openConnectionId) return;
      if (this.reader === currentReader) this.reader = undefined;
      const report = timedOut
        ? new Error(`Request timeout after ${this.transport.timeoutMs}ms`)
        : toError(error);
      this.reportError(report);
      if (this.isDisposed || this.connectionId !== openConnectionId) return;
      this.setState('reconnecting');
      this.scheduleReconnect(openConnectionId);
    } finally {
      // Generation-local establish timer cleanup on every exit path. Early
      // returns from stale-generation checks (auth/hash/fetch/response
      // validation) bypass the catch, so without this the timer/controller
      // would linger until the timeout fires. This does NOT abort the
      // controller: established SSE connections cleared the timer before the
      // read loop, so this is a no-op for live streams.
      clearEstablishTimer();
    }
  }

  private processChunk(chunk: Uint8Array | undefined, connectionId: number, done: boolean): void {
    if (this.isDisposed || this.connectionId !== connectionId) return;
    this.parser.feed(
      chunk,
      done,
      {
        onData: (data) => this.handleEvent(data, connectionId),
        onError: (error) => this.reportError(error),
      },
    );
  }

  private handleEvent(data: string, connectionId: number): void {
    if (this.isDisposed || this.connectionId !== connectionId) return;

    let envelope: unknown;
    try {
      envelope = JSON.parse(data);
    } catch {
      this.reportError(new Error('Malformed SSE event data'));
      return;
    }

    if (!isSseEnvelope(envelope)) {
      this.reportError(new Error('Invalid SSE envelope'));
      return;
    }

    const realtime = envelope.data;
    if (realtime.id !== this.id) {
      return;
    }

    // Control envelopes (subscribe, ping, pong, unsubscribe) do not indicate
    // meaningful data/update stability and must not reset the retry counter.
    if (realtime.op === 'subscribe') {
      // Consume the backend's maxEventSize negotiation, bounded by the client's
      // configured safety ceiling (the parser only tightens, never loosens).
      if (realtime.maxEventSize !== undefined) {
        this.parser.negotiateMaxEventDataLength(realtime.maxEventSize);
      }
      return;
    }
    if (realtime.op === 'ping' || realtime.op === 'pong' || realtime.op === 'unsubscribe') {
      return;
    }

    if (realtime.op === 'message') {
      if (realtime.payload === undefined) {
        this.reportError(new Error('Message envelope missing payload'));
        return;
      }

      let decoded: unknown;
      try {
        decoded = decodeValue(realtime.payload);
      } catch (error) {
        this.reportError(toError(error));
        return;
      }

      // Only reset the reconnect counter after the payload has been decoded
      // and validated; an undecodable message must not signal a healthy
      // connection or prevent exhaustion of maxReconnects.
      this.reconnectAttempt = 0;

      if (isStructuredError(decoded)) {
        const error = new PBVexError(decoded);
        this.updateAll({ data: undefined, error, isLoading: false });
        this.reportError(error);
        return;
      }

      this.updateAll({ data: decoded, error: null, isLoading: false });
    }
  }

  private scheduleReconnect(causeConnectionId: number): void {
    if (this.isDisposed || this.watchers.length === 0) {
      this.dispose();
      return;
    }

    if (this.reconnectAttempt >= this.maxReconnects) {
      this.reportError(new Error('Realtime reconnect limit reached'));
      this.dispose();
      return;
    }

    const delay = Math.min(this.initialReconnectDelayMs * 2 ** this.reconnectAttempt, this.maxReconnectDelayMs);
    const scheduledConnectionId = this.connectionId;

    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = undefined;
      // Only reconnect if the connection this backoff was for is still current.
      if (this.isDisposed || this.connectionId !== scheduledConnectionId || this.connectionId !== causeConnectionId) {
        return;
      }
      this.reconnectAttempt += 1;
      this.openConnection();
    }, delay);
  }

  dispose(): void {
    if (this.isDisposed) return;
    this.isDisposed = true;
    this.clearReconnectTimer();
    this.reader?.cancel().catch(() => {});
    this.reader = undefined;
    this.abortController?.abort();
    this.setState('disconnected');
    this.transport.removeSubscription(this.subscriptionKey);
  }
}

export class FetchRealtimeTransport implements RealtimeTransport {
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

  private readonly subscriptions = new Map<string, Subscription>();
  private closed = false;

  constructor(options: FetchRealtimeTransportOptions) {
    const fetchFn = options.fetch ?? (typeof fetch !== 'undefined' ? fetch : undefined as never);
    if (!fetchFn) {
      throw new Error('No fetch implementation available');
    }
    this.fetchFn = fetchFn;
    this.baseUrl = new URL('/', options.baseUrl).toString();
    this.getAuthToken = options.getAuthToken;
    this.realtimePath = options.realtimePath ?? '/api/pbvex/realtime';
    this.maxReconnects = validateReconnectValue(options.maxReconnects, 5, 'maxReconnects', MAX_RECONNECTS);
    this.initialReconnectDelayMs = validateDelay(options.initialReconnectDelayMs, 500, 'initialReconnectDelayMs', MAX_INITIAL_RECONNECT_DELAY_MS);
    this.maxReconnectDelayMs = validateDelay(options.maxReconnectDelayMs, 30000, 'maxReconnectDelayMs', MAX_RECONNECT_DELAY_MS);
    if (this.maxReconnectDelayMs < this.initialReconnectDelayMs) {
      throw new TypeError('maxReconnectDelayMs must be >= initialReconnectDelayMs');
    }
    this.maxFunctionArgsBytes = validateLimit(options.limits?.maxFunctionArgsBytes, DEFAULT_CONFIG.maxFunctionArgsBytes, 'maxFunctionArgsBytes');
    this.maxReturnValueBytes = validateLimit(options.limits?.maxReturnValueBytes, DEFAULT_CONFIG.maxReturnValueBytes, 'maxReturnValueBytes');
    // Allow the full path length and the id in the realtime POST body.
    this.maxRealtimeBodyBytes = this.maxFunctionArgsBytes + MAX_PATH_LENGTH + 64 + 100;
    this.maxResponseBodyBytes = this.maxReturnValueBytes + 4096;
    this.maxSseEventDataLength = this.maxReturnValueBytes + 4096;
    this.maxSseLineLength = this.maxSseEventDataLength + 6;
    this.timeoutMs = validateTimeoutMs(options.timeoutMs, DEFAULT_CONFIG.defaultRequestTimeoutMs);
  }

  async computeSubscriptionId(path: string, canonicalArgs: string): Promise<string> {
    return hashSha256(`v1:${path}:${canonicalArgs}`);
  }

  get connectionState(): ConnectionState {
    if (this.subscriptions.size === 0) return 'disconnected';
    let connected = false;
    let connecting = false;
    let reconnecting = false;
    for (const subscription of this.subscriptions.values()) {
      const state = subscription.getState();
      if (state === 'connected') connected = true;
      if (state === 'connecting') connecting = true;
      if (state === 'reconnecting') reconnecting = true;
    }
    if (connected) return 'connected';
    if (connecting) return 'connecting';
    if (reconnecting) return 'reconnecting';
    return 'disconnected';
  }

  watch<Args, Return>(path: string, args: Args, options: WatchOptions<Return>): Unsubscribe {
    if (this.closed) {
      return () => {};
    }

    if (args === 'skip') {
      throw new Error('skip is reserved by adapter hooks and cannot be passed to realtime core');
    }

    if (byteLength(path) > MAX_PATH_LENGTH) {
      throw new Error(`Realtime path exceeds ${MAX_PATH_LENGTH} bytes`);
    }

    const encodedArgs = defaultEncodeArgs(args);
    const canonicalArgs = canonicalJson(encodedArgs);
    if (byteLength(canonicalArgs) > this.maxFunctionArgsBytes) {
      throw new Error(`Realtime subscription args exceed ${this.maxFunctionArgsBytes} bytes`);
    }

    const subscriptionKey = `${path}:${canonicalArgs}`;

    let subscription = this.subscriptions.get(subscriptionKey);
    if (!subscription) {
      subscription = new Subscription(this, subscriptionKey, path, args, encodedArgs, canonicalArgs, options as WatchOptions<unknown>);
      this.subscriptions.set(subscriptionKey, subscription);
    }

    return subscription.addWatcher(options);
  }

  removeSubscription(subscriptionKey: string): void {
    this.subscriptions.delete(subscriptionKey);
  }

  refreshAuth(): void {
    if (this.closed) return;
    for (const subscription of this.subscriptions.values()) {
      subscription.refreshAuth();
    }
  }

  close(): void {
    this.closed = true;
    for (const subscription of this.subscriptions.values()) {
      subscription.dispose();
    }
    this.subscriptions.clear();
  }
}
