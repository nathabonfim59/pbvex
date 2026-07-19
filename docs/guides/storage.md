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

`StorageId` is a branded opaque type at the server/client API boundary. Use `v.image()` for schema fields that own images with predefined thumbnail variants; use your chosen representation for generic files. Ordinary `v.image()` value validation checks only canonical `StorageId` syntax under a valid policy descriptor. It does not check object existence, bytes, ownership, or whether the ID came from a schema-bound image upload.

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

The upload endpoint is the generated URL below the configured storage base path (default `/api/pbvex/storage/upload/{token}`), not an endpoint to invent or reuse. The token expires, is single-use, and has configured content-type/size limits. Its response includes `storageId` and persisted `metadata`. Size, checksum, detected content type, and `createdBy` are server-derived; `createdBy` is the token identifier that requested the upload URL, or an empty string for anonymous issuance. It is audit metadata that applications may check, not automatic storage authorization. `filename` and a generic file's filename-derived `extension` remain client-supplied naming data. Upload failures include invalid/expired/consumed/pending token, unsupported content, too-large, and storage-full cases. Ask for a new URL rather than retrying a consumed token.

## Schema-declared images

Declare the accepted raster formats and every resize variant in the collection schema:

```ts
export default defineSchema({
  photos: defineTable({
    image: v.image({
      thumbs: ['320x240f', '1600x0'],
      mimeTypes: ['image/jpeg', 'image/png', 'image/webp'],
    }),
  }),
});

export const getImageUploadUrl = mutation({
  handler: (ctx) => ctx.storage.generateUploadUrl({
    table: 'photos',
    field: 'image',
  }),
});
```

The upload token snapshots that field policy. PBVex detects MIME from the bytes, decodes the image, and persists trusted extension, width, height, size, and checksum metadata. A filename extension or request `Content-Type` is not proof that bytes are an image.

Set the `thumb` query parameter on any returned image URL. The parameter uses PocketBase syntax: `WxH` crops from center, `WxHt` crops from top, `WxHb` crops from bottom, `WxHf` fits without cropping, and `0xH` or `Wx0` preserves aspect ratio. Only exact `thumbs` entries persisted with that image are accepted. Variants are generated lazily through PocketBase's configured local/S3 filesystem and cached as storage objects:

```ts
const url = await ctx.storage.getUrl(photo.image, { mode: 'public' });
if (!url) return null;
const thumbnail = new URL(url);
thumbnail.searchParams.set('thumb', '320x240f');
return thumbnail.toString();
```

Use `URL.searchParams` because signed identity and capability URLs already contain query parameters. Predeclare variants for avatars/cards/lists and use them for routine rendering; reserve the original URL without `thumb` for explicit zoom or download.

`ctx.storage.getMetadata(id)` returns persisted file metadata and, for image-policy uploads, trusted `kind: 'image'`, byte-derived `extension`, `width`, `height`, and `thumbs`. Generic and image `filename` values remain client-supplied display data. Authorize metadata access just like URL access.

See [Image uploads and resizing](./image-resizing.md) for the complete selector syntax, URL handling across access modes, generation and caching behavior, schema changes, migration guidance, and failure cases.

## Server capabilities

- Queries: `ctx.storage.getUrl(id)` and `getMetadata(id)`.
- Mutations and actions (including HTTP actions): `generateUploadUrl`, `getUrl`, `getMetadata`, and `delete`.

`getUrl(id)` returns a download URL or `null` when the ID is invalid, missing, or deleted. A valid ID does not establish ownership or publication permission: authorize against the owning document before URL issuance. By default, a URL created by an authenticated function is identity-bound and the download request must carry the same bearer token. An identity URL issued anonymously has an empty subject: it works only on an anonymous request with no `Authorization` header, and possession grants access until expiry. Use `getUrl(id, { mode: 'capability' })` for a short-lived bearer URL suitable for `<img>`, `<video>`, navigation, or download links. Use `getUrl(id, { mode: 'public' })` only for intentionally public immutable assets: it returns a stable, queryless bearer URL designed for browser and shared CDN caches.

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

There is no `ctx.storage.get`, public listing API, arbitrary resize API, or server-side byte streaming API in v1. Keep ownership and application-specific metadata in your own documents.

## Download behavior and backends

All storage URLs support GET/HEAD, conditional requests, and ranges. Identity mode is the default: authenticated URLs are short-lived, caller-bound, and privately cacheable through expiry; anonymous identity URLs are signed, require an anonymous request, and are publicly cacheable through expiry. Capability mode is a short-lived bearer claim publicly cacheable through expiry. Neither short-lived mode has per-link revocation; expiry or object deletion ends origin access. Public mode returns the same unguessable `/public/{token}/blob.bin` URL on every call and across signing-key rotation, with `public`, `s-maxage`, and revalidation cache directives. Public URLs remain valid until file deletion and may remain available from caches for the configured public cache TTL after deletion at the origin. `getUrl` does not itself decide ownership—your function must authorize access or publication before returning a URL.

PBVex storage uses PocketBase’s configured filesystem. Local storage persists in the server data directory; a configured S3-compatible filesystem stores objects remotely, while PBVex retains metadata and signed-URL authorization in its database. Back up the database and object store together. See [the self-hosting guide](../self-hosting.md#storage-configuration) for limits and backend configuration.
