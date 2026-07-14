export class PBVexError extends Error {
    structuredError;
    error = true;
    code;
    message;
    details;
    requestId;
    constructor(structuredError) {
        super(structuredError.message);
        this.structuredError = structuredError;
        this.name = 'PBVexError';
        this.code = structuredError.code;
        this.message = structuredError.message;
        this.details = structuredError.details;
        this.requestId = structuredError.requestId;
    }
}
//# sourceMappingURL=errors.js.map