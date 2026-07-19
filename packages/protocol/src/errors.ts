import { ERROR_CODES, type ErrorCode, type StructuredError } from './types.js';

const errorCodes = new Set<string>(ERROR_CODES);

export function isErrorCode(value: unknown): value is ErrorCode {
  return typeof value === 'string' && errorCodes.has(value);
}

export function isStructuredError(value: unknown): value is StructuredError {
  return (
    typeof value === 'object' &&
    value !== null &&
    (value as Record<string, unknown>).error === true &&
    isErrorCode((value as Record<string, unknown>).code) &&
    typeof (value as Record<string, unknown>).message === 'string'
  );
}

export function structuredError(
  code: ErrorCode,
  message: string,
  options?: { details?: unknown[]; data?: StructuredError['data']; requestId?: string }
): StructuredError {
  return {
    error: true,
    code,
    message,
    details: options?.details,
    data: options?.data,
    requestId: options?.requestId,
  };
}

export function errorToJson(error: StructuredError): { [key: string]: unknown } {
  return {
    error: error.error,
    code: error.code,
    message: error.message,
    ...(error.details !== undefined ? { details: error.details } : {}),
    ...(error.data !== undefined ? { data: error.data } : {}),
    ...(error.requestId !== undefined ? { requestId: error.requestId } : {}),
  };
}
