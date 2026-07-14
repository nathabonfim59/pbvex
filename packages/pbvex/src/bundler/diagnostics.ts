export class DiagnosticError extends Error {
  constructor(
    public readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = 'DiagnosticError';
  }
}

export function diagnostic(code: string, message: string, detail: string): DiagnosticError {
  return new DiagnosticError(code, `${message}: ${detail}`);
}
