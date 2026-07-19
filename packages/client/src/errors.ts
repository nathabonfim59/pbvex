import { decodeValue, type ErrorCode, type PbvexValue, type StructuredError } from '@pbvex/protocol';

export class PBVexError extends Error {
  readonly error = true as const;
  readonly code: ErrorCode;
  readonly message: string;
  readonly details?: unknown[];
  readonly data?: PbvexValue;
  readonly requestId?: string;

  constructor(readonly structuredError: StructuredError) {
    super(structuredError.message);
    this.name = 'PBVexError';
    this.code = structuredError.code;
    this.message = structuredError.message;
    this.details = structuredError.details;
    if (structuredError.data !== undefined) {
      try {
        this.data = decodeValue(structuredError.data);
      } catch {
        // Realtime decodes the complete payload before recognizing the envelope.
        this.data = structuredError.data as PbvexValue;
      }
    }
    this.requestId = structuredError.requestId;
  }
}
