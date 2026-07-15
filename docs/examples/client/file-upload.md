# File upload flow

`@pbvex/client` does not expose a direct file upload method. A server-side mutation or action first calls `ctx.storage.generateUploadUrl()` (see the [Storage guide](../../guides/storage.md)); the browser then uploads directly to that one-time URL.

## Server mutation that returns an upload URL

```ts
// pbvex/files.ts
import { mutation } from './_generated/server.js';

export const getUploadUrl = mutation({
  handler: async (ctx) => {
    const url = await ctx.storage.generateUploadUrl();
    return { url };
  },
});
```

## Client upload flow

```ts
import { Client, type StorageId, type StorageUploadResponse } from '@pbvex/client';
import { api } from './pbvex/_generated/api.js';

const client = new Client('http://localhost:8090');

async function uploadFile(file: File) {
  // 1. Request a one-time upload URL from the server.
  const { url } = await client.mutation(api.files.getUploadUrl);

  // 2. Upload the file directly to the returned URL.
  const upload = await fetch(url, {
    method: 'POST',
    body: file,
    headers: {
      'Content-Type': file.type || 'application/octet-stream',
    },
  });

  if (!upload.ok) {
    throw new Error(`Upload failed: ${upload.status}`);
  }

  // 3. The upload endpoint returns an opaque StorageId.
  const { storageId } = await upload.json() as StorageUploadResponse;

  // 4. Persist the storage id in a record.
  await client.mutation(api.files.attach, { storageId });
}

async function downloadUrl(storageId: StorageId) {
  return client.query(api.files.getUrl, { storageId });
}
```

## Limits

- `ClientLimits.maxUploadBytes` is accepted for forward compatibility but is not enforced by the client.
- The actual upload size limit is enforced by the backend (`--storageMaxFileSize` / `PBVEX_STORAGE_MAX_FILE_SIZE`).
- The upload endpoint returns a structured JSON error on failure. It is a direct `fetch`, so inspect that response yourself; `PBVexError` wrapping applies to calls made through `Client`.

## Notes

- Upload with `POST`, a raw request body, and a valid `Content-Type`. The public HTTP contract is the same regardless of PocketBase's configured file backend.
- `StorageId` is an opaque branded string; pass it through without constructing or parsing.
- Delete a stored object through a server mutation or action that calls `ctx.storage.delete(id)`.
