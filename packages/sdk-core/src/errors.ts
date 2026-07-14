import type { ErrorCode, StructuredError } from '@pbvex/protocol';

export class PBVexError extends Error {
  readonly error = true as const;
  readonly code: ErrorCode;
  readonly message: string;
  readonly details?: unknown[];
  readonly requestId?: string;

  constructor(readonly structuredError: StructuredError) {
    super(structuredError.message);
    this.name = 'PBVexError';
    this.code = structuredError.code;
    this.message = structuredError.message;
    this.details = structuredError.details;
    this.requestId = structuredError.requestId;
  }
}
