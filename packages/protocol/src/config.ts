import type { DeploymentConfig } from './types.js';

export const MAX_DEPLOYMENT_UPLOAD_BYTES = 64 * 1024 * 1024;
export const MAX_FUNCTION_ARGS_LIMIT = 16 * 1024 * 1024;
export const MAX_RETURN_VALUE_LIMIT = 16 * 1024 * 1024;

export const DEFAULT_CONFIG: DeploymentConfig = {
  httpPathPrefix: '/api/pbvex',
  realtimePath: '/api/pbvex/realtime',
  maxUploadBytes: MAX_DEPLOYMENT_UPLOAD_BYTES,
  maxFunctionArgsBytes: 1024 * 1024,
  maxReturnValueBytes: 1024 * 1024,
  defaultRequestTimeoutMs: 30000,
};
