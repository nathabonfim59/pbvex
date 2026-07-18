# File storage

Storage is a two-step flow: a mutation or action creates a short-lived, single-use upload URL; the client uploads bytes to that URL and receives a `StorageId`; store that ID in your own document.

```ts
// pbvex/files.ts
import { mutation, query } from './_generated/server';
import { v } from 'pbvex/values';

export const createUpload = mutation({
  returns: v.string(),
  handler: (ctx) => ctx.storage.generateUploadUrl(),
});

export const attach = mutation({
  args: { messageId: v.id('messages'), fileId: v.string() },
  handler: async (ctx, args) => {
    // A StorageId is opaque; validate authorization before attaching it.
    await ctx.db.patch(args.messageId, { fileId: args.fileId });
  },
});

// `messages` must declare `fileId: v.optional(v.string())` in pbvex/schema.ts.
```

`StorageId` is a branded opaque type at the server/client API boundary. If a schema stores it, use your chosen representation consistently; current storage APIs do not provide a `v.storageId()` validator.

```ts
import { Client, type StorageUploadResponse } from '@pbvex/client';
import type { Id } from './pbvex/_generated/dataModel';
import { api } from './pbvex/_generated/api';

async function uploadFile(client: Client, messageId: Id<'messages'>, file: File) {
  const uploadUrl = await client.mutation(api.files.createUpload);
  const response = await fetch(uploadUrl, {
    method: 'POST',
    headers: {
      'Content-Type': file.type || 'application/octet-stream',
      'X-Upload-Filename': file.name,
    },
    body: file,
  });
  if (!response.ok) throw new Error(`upload failed: ${response.status}`);
  const { storageId } = await response.json() as StorageUploadResponse;
  await client.mutation(api.files.attach, { messageId, fileId: storageId });
}
```

The upload endpoint is the generated URL below the configured storage base path (default `/api/pbvex/storage/upload/{token}`), not an endpoint to invent or reuse. The token expires, is single-use, and has configured content-type/size limits. Upload failures include invalid/expired/consumed/pending token, unsupported content, too-large, and storage-full cases. Ask for a new URL rather than retrying a consumed token.

## Server capabilities

- Queries: `ctx.storage.getUrl(id)` only.
- Mutations and actions (including HTTP actions): `generateUploadUrl`, `getUrl`, and `delete`.

`getUrl(id)` returns a download URL or `null` when the ID is invalid, missing, or deleted. By default, a URL created by an authenticated function is identity-bound and the download request must carry the same bearer token. Use `getUrl(id, { mode: 'capability' })` for a short-lived regular URL suitable for `<img>`, `<video>`, navigation, or download links. Use `getUrl(id, { mode: 'public' })` only for intentionally public immutable assets: it returns a stable, queryless bearer URL designed for browser and shared CDN caches.

`delete(id)` removes the file; within a mutation, metadata deletion is transactional and irreversible blob cleanup occurs after a successful commit (with durable cleanup recovery).

```ts
export const download = query({
  args: { fileId: v.string() },
  handler: async (ctx, { fileId }) => {
    // Check that the caller may access the owning document before returning this URL.
    return ctx.storage.getUrl(
      fileId as import('pbvex/server').StorageId,
      { mode: 'capability' },
    );
  },
});
```

There is no `ctx.storage.get`, metadata/read API, public listing API, custom upload policy API, or server-side byte streaming API in v1. Keep content type, original filename, ownership, and application metadata in your own documents if you need to query them.

## Download behavior and backends

All storage URLs support GET/HEAD, conditional requests, and ranges. Identity mode is the default and binds a short-lived URL to the authenticated token identifier that requested it. Capability mode signs an explicit short-lived bearer-access claim into the URL. Public mode returns the same unguessable `/public/{token}/blob.bin` URL on every call and across signing-key rotation, with `public`, `s-maxage`, and revalidation cache directives. Public URLs remain valid until file deletion and may remain available from caches for the configured public cache TTL after deletion at the origin. `getUrl` does not itself decide ownership—your function must authorize access or publication before returning a URL.

PBVex storage uses PocketBase’s configured filesystem. Local storage persists in the server data directory; a configured S3-compatible filesystem stores objects remotely, while PBVex retains metadata and signed-URL authorization in its database. Back up the database and object store together. See [the self-hosting guide](../self-hosting.md#storage-configuration) for limits and backend configuration.
