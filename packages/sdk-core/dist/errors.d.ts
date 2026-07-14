import type { ErrorCode, StructuredError } from '@pbvex/protocol';
export declare class PBVexError extends Error {
    readonly structuredError: StructuredError;
    readonly error: true;
    readonly code: ErrorCode;
    readonly message: string;
    readonly details?: unknown[];
    readonly requestId?: string;
    constructor(structuredError: StructuredError);
}
//# sourceMappingURL=errors.d.ts.map