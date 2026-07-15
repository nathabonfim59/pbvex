import assert from 'node:assert/strict';
import test from 'node:test';

import { ERROR_CODES, isErrorCode, isStructuredError, structuredError } from './index.js';
import type { ErrorCode, StorageId, StorageUploadResponse } from './index.js';

test('storage error codes are part of the strict shared wire contract', () => {
  const storageCodes: ErrorCode[] = [
    'upload_expired',
    'upload_consumed',
    'upload_pending',
    'upload_too_large',
    'invalid_content',
    'storage_full',
  ];
  for (const code of storageCodes) {
    assert.equal(ERROR_CODES.includes(code), true);
    assert.equal(isErrorCode(code), true);
    assert.equal(isStructuredError(structuredError(code, 'storage error')), true);
  }
  assert.equal(isErrorCode('unknown_storage_error'), false);
  assert.equal(isStructuredError({ error: true, code: 'unknown_storage_error', message: 'bad' }), false);
});

test('storage upload responses carry the branded identifier at the type boundary', () => {
  const storageId = 'pbv_0123456789abcdef0123456789abcdef' as StorageId;
  const response: StorageUploadResponse = { storageId };
  const asString: string = response.storageId;
  assert.equal(asString, storageId);

  // @ts-expect-error arbitrary strings are not StorageId capabilities
  const invalid: StorageUploadResponse = { storageId: 'plain-string' };
  assert.equal(invalid.storageId, 'plain-string');
});
