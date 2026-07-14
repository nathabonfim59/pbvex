import { isJsonValue } from './validators.js';
import { DEFAULT_CONFIG } from './config.js';
import { MAX_PATH_LENGTH } from './validators.js';
import type { HttpMethod, HttpEnvelope, HttpResponse, RealtimeOp, RealtimeEnvelope, SseEnvelope } from './types.js';

export const MAX_FUNCTION_ARGS_BYTES = DEFAULT_CONFIG.maxFunctionArgsBytes;
export const MAX_RETURN_VALUE_BYTES = DEFAULT_CONFIG.maxReturnValueBytes;
export const MAX_UPLOAD_BYTES = DEFAULT_CONFIG.maxUploadBytes;
export const MAX_REQUEST_TIMEOUT_MS = DEFAULT_CONFIG.defaultRequestTimeoutMs;

// Response bodies wrap a return value that is bounded by the backend; allow generous
// envelope overhead so an exactly-max return value is not falsely rejected.
export const MAX_RESPONSE_BODY_BYTES = MAX_RETURN_VALUE_BYTES + 4096;

// Realtime SSE event data carries a RealtimeEnvelope whose payload is bounded by
// MAX_RETURN_VALUE_BYTES; the line includes the "data: " field prefix.
export const MAX_SSE_EVENT_DATA_LENGTH = MAX_RETURN_VALUE_BYTES + 4096;
export const MAX_SSE_LINE_LENGTH = MAX_SSE_EVENT_DATA_LENGTH + 6;

// Realtime subscription request body: args can be up to MAX_FUNCTION_ARGS_BYTES, plus
// the subscription id, path, and JSON envelope. Allow MAX_PATH_LENGTH for the path,
// 64 bytes for the SHA-256 id, and 100 bytes of JSON envelope overhead.
export const MAX_REALTIME_ARGS_BYTES = MAX_FUNCTION_ARGS_BYTES;
export const MAX_REALTIME_BODY_BYTES = MAX_FUNCTION_ARGS_BYTES + MAX_PATH_LENGTH + 64 + 100;

export interface ParsedContentType {
  type: string;
  subtype: string;
  mediaType: string;
  parameters: Map<string, string>;
}

// RFC 7230 token characters used by media-type grammar.
const TOKEN_CHARS = new Set(
  "!#$%&'*+-.^_`|~0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
);

function isTokenChar(ch: string): boolean {
  return TOKEN_CHARS.has(ch);
}

function isToken(value: string): boolean {
  if (value.length === 0) return false;
  for (let i = 0; i < value.length; i += 1) {
    if (!isTokenChar(value[i])) return false;
  }
  return true;
}

function skipOWS(value: string, index: number): number {
  while (index < value.length && (value[index] === ' ' || value[index] === '\t')) {
    index += 1;
  }
  return index;
}

function parseToken(value: string, index: number): { token: string; next: number } | undefined {
  const start = index;
  while (index < value.length && isTokenChar(value[index])) {
    index += 1;
  }
  if (start === index) return undefined;
  return { token: value.slice(start, index), next: index };
}

function parseQuotedString(value: string, index: number): { value: string; next: number } | undefined {
  if (value[index] !== '"') return undefined;
  index += 1;
  let result = '';
  while (index < value.length) {
    const ch = value[index];
    if (ch === '"') {
      index += 1;
      return { value: result, next: index };
    }
    if (ch === '\\') {
      index += 1;
      if (index >= value.length) return undefined;
      const escaped = value[index];
      const escapedCp = escaped.codePointAt(0) ?? 0;
      // quoted-pair: HTAB, SP, VCHAR (0x21-0x7E), or obs-text (0x80+).
      // Reject NUL, all other CTLs, and DEL (0x7F).
      if (escaped === '\t' || escaped === ' ' || (escapedCp >= 0x21 && escapedCp <= 0x7e) || escapedCp >= 0x80) {
        result += escaped;
      } else {
        return undefined;
      }
      index += 1;
      continue;
    }
    const cp = ch.codePointAt(0) ?? 0;
    // qdtext: HTAB, SP, VCHAR, obs-text. Reject NUL, all CTLs, and DEL.
    if (cp === 0x7f || (cp < 0x20 && cp !== 0x09 && cp !== 0x20)) {
      return undefined;
    }
    result += ch;
    index += 1;
  }
  return undefined;
}

function parseParameterValue(value: string, index: number): { value: string; next: number } | undefined {
  if (value[index] === '"') {
    return parseQuotedString(value, index);
  }
  const tokenResult = parseToken(value, index);
  if (tokenResult) return { value: tokenResult.token, next: tokenResult.next };
  return undefined;
}

/**
 * Parse a Content-Type header as a media type per RFC 7231/7230:
 * `type "/" subtype *( OWS ";" OWS parameter )`
 * where type and subtype are tokens and parameter values are tokens or
 * quoted-strings. Parameter names are normalized to lowercase.
 */
export function parseContentType(value: string): ParsedContentType | undefined {
  let index = 0;
  index = skipOWS(value, index);
  const typeResult = parseToken(value, index);
  if (!typeResult) return undefined;
  const type = typeResult.token.toLowerCase();
  index = typeResult.next;

  // type and subtype are separated by "/" with no OWS.
  if (value[index] !== '/') return undefined;
  index += 1;

  const subtypeResult = parseToken(value, index);
  if (!subtypeResult) return undefined;
  const subtype = subtypeResult.token.toLowerCase();
  index = subtypeResult.next;

  const parameters = new Map<string, string>();

  while (index < value.length) {
    index = skipOWS(value, index);
    if (value[index] !== ';') break;
    index += 1;
    index = skipOWS(value, index);

    const nameResult = parseToken(value, index);
    if (!nameResult) return undefined;
    const name = nameResult.token.toLowerCase();
    index = nameResult.next;

    // parameter name and value are separated by "=" with no OWS.
    if (value[index] !== '=') return undefined;
    index += 1;

    const valueResult = parseParameterValue(value, index);
    if (!valueResult) return undefined;
    if (parameters.has(name)) return undefined;
    parameters.set(name, valueResult.value);
    index = valueResult.next;
  }

  index = skipOWS(value, index);
  if (index !== value.length) return undefined;

  return {
    type,
    subtype,
    mediaType: `${type}/${subtype}`,
    parameters,
  };
}

function isReadableStream(value: unknown): value is ReadableStream<Uint8Array> {
  return (
    value !== null &&
    typeof value === 'object' &&
    typeof (value as { getReader: unknown }).getReader === 'function'
  );
}

export async function readBoundedText(
  response: Response,
  maxBytes = MAX_RESPONSE_BODY_BYTES,
  signal?: AbortSignal,
): Promise<string> {
  if (signal?.aborted) throw abortError(signal.reason);
  const body = response.body;
  if (!body) return '';
  if (!isReadableStream(body)) {
    return '';
  }
  const reader = body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  // Read up to one byte beyond the limit so we can distinguish an exact-max
  // body from a truncated one.
  const limit = maxBytes + 1;
  let onAbort: (() => void) | undefined;
  const abortPromise = signal
    ? new Promise<never>((_, reject) => {
        onAbort = () => {
          reader.cancel().catch(() => {});
          reject(abortError(signal.reason));
        };
        signal.addEventListener('abort', onAbort, { once: true });
      })
    : undefined;
  if (abortPromise) {
    abortPromise.catch(() => {});
  }
  try {
    for (;;) {
      const readPromise = reader.read();
      const { done, value } =
        abortPromise !== undefined
          ? await Promise.race([readPromise, abortPromise])
          : await readPromise;
      if (done) break;
      if (value) {
        const remaining = limit - total;
        if (remaining <= 0) {
          await reader.cancel().catch(() => {});
          throw new Error(`Response body exceeds ${maxBytes} bytes`);
        }
        const take = Math.min(value.length, remaining);
        chunks.push(value.subarray(0, take));
        total += take;
        if (total > maxBytes) {
          await reader.cancel().catch(() => {});
          throw new Error(`Response body exceeds ${maxBytes} bytes`);
        }
      }
    }
  } finally {
    if (signal && onAbort) {
      signal.removeEventListener('abort', onAbort);
    }
    await reader.closed.catch(() => {});
    reader.releaseLock();
  }
  const decoder = new TextDecoder('utf-8', { fatal: true });
  try {
    return decoder.decode(concatUint8Arrays(chunks, total));
  } catch {
    throw new Error('Invalid UTF-8 in response body');
  }
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

function concatUint8Arrays(chunks: Uint8Array[], totalLength: number): Uint8Array {
  const out = new Uint8Array(totalLength);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.length;
  }
  return out;
}

export function isHttpMethod(value: unknown): value is HttpMethod {
  return value === 'GET' || value === 'POST' || value === 'PUT' || value === 'PATCH' || value === 'DELETE';
}

export function isHttpEnvelope(value: unknown): value is HttpEnvelope {
  if (typeof value !== 'object' || value === null) return false;
  const o = value as Record<string, unknown>;
  return (
    isHttpMethod(o.method) &&
    typeof o.path === 'string' &&
    (o.headers === undefined || isStringRecord(o.headers)) &&
    (o.query === undefined || isQueryRecord(o.query)) &&
    (o.body === undefined || isJsonValue(o.body))
  );
}

export function isHttpResponse(value: unknown): value is HttpResponse {
  if (typeof value !== 'object' || value === null) return false;
  const o = value as Record<string, unknown>;
  return (
    typeof o.status === 'number' &&
    Number.isInteger(o.status) &&
    o.status >= 100 &&
    o.status <= 599 &&
    (o.headers === undefined || isStringRecord(o.headers)) &&
    (o.body === null || o.body === undefined || isJsonValue(o.body))
  );
}

export function isRealtimeOp(value: unknown): value is RealtimeOp {
  return value === 'message' || value === 'subscribe' || value === 'unsubscribe' || value === 'ping' || value === 'pong';
}

export function isRealtimeEnvelope(value: unknown): value is RealtimeEnvelope {
  if (typeof value !== 'object' || value === null) return false;
  const o = value as Record<string, unknown>;
  if (typeof o.id !== 'string' || !isRealtimeOp(o.op)) return false;
  if (o.path !== undefined && typeof o.path !== 'string') return false;
  if (o.args !== undefined && !isJsonValue(o.args)) return false;
  if (o.payload !== undefined && !isJsonValue(o.payload)) return false;
  if (
    o.maxEventSize !== undefined &&
    (typeof o.maxEventSize !== 'number' || !Number.isFinite(o.maxEventSize) || o.maxEventSize <= 0 || !Number.isInteger(o.maxEventSize))
  ) {
    return false;
  }
  // Message envelopes must carry a payload; explicit null is valid.
  if (o.op === 'message' && (!('payload' in o) || o.payload === undefined)) return false;
  return true;
}

export function isSseEnvelope(value: unknown): value is SseEnvelope {
  if (typeof value !== 'object' || value === null) return false;
  const o = value as Record<string, unknown>;
  return (
    (o.event === undefined || typeof o.event === 'string') &&
    (o.id === undefined || typeof o.id === 'string') &&
    isRealtimeEnvelope(o.data)
  );
}

function isStringRecord(value: unknown): value is Record<string, string> {
  if (typeof value !== 'object' || value === null) return false;
  for (const [k, v] of Object.entries(value)) {
    if (!k || typeof v !== 'string') return false;
  }
  return true;
}

function isQueryRecord(value: unknown): value is Record<string, string | string[]> {
  if (typeof value !== 'object' || value === null) return false;
  for (const [k, v] of Object.entries(value)) {
    if (!k) return false;
    if (typeof v !== 'string' && !(Array.isArray(v) && v.every((x) => typeof x === 'string'))) return false;
  }
  return true;
}
