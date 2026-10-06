export type ErrorCode = 'network' | 'unauthorized' | 'conflict' | 'invalid_data' | 'unavailable' | 'pairing_uncertain' | 'permission' | 'unsupported' | 'account_mismatch' | 'storage' | 'retry_exhausted';
export class AppError extends Error {
  constructor(public readonly code: ErrorCode, public readonly retryable = false, public readonly retryAfterMs = 0) { super(code); }
}
export function safeError(error: unknown): ErrorCode { return error instanceof AppError ? error.code : 'unavailable'; }
