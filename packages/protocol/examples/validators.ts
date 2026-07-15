import {
  isIdentifier,
  isSha256Hex,
  isJsonValue,
  isStructuredError,
  structuredError,
  validateManifest,
  type StructuredError,
} from '@pbvex/protocol';

// Desired validator call sites used by backend/CLI and SDK tooling.

const validName = isIdentifier('listTasks');
const validHash = isSha256Hex('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855');
const validJson = isJsonValue({ tasks: [{ _id: 't1' }] });

const err: StructuredError = structuredError('invalid_manifest', 'manifest failed validation', {
  details: ['missing functions'],
  requestId: 'req_123',
});

const isErr: boolean = isStructuredError(err);

export { validName, validHash, validJson, err, isErr };
