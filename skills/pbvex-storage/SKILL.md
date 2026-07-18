---
name: pbvex-storage
description: Implement, review, troubleshoot, or operate PBVex file and image storage, including upload URLs, StorageId fields, trusted metadata, identity/capability/public downloads, CDN caching, schema-declared thumbnails, local or S3 object storage, limits, deletion, and migration behavior.
---

# PBVex file and image storage

Store only the opaque `StorageId` in a PBVex document. Original bytes and generated thumbnails live in PocketBase's configured local or S3-compatible object storage; SQLite holds the ID, object key, status, policy, and metadata. Before upload, authorize the caller against the intended parent/resource; after attachment, authorize through the owning document before returning metadata/URLs or deleting.

## Upload files and images

Generic files use `ctx.storage.generateUploadUrl()`. Images use a top-level schema field and a policy-bound URL:

```ts
image: v.image({
  thumbs: ['320x240f', '1600x0'],
  mimeTypes: ['image/jpeg', 'image/png', 'image/webp'],
});

const url = await ctx.storage.generateUploadUrl({
  table: 'photos',
  field: 'image',
});
```

The client POSTs bytes once to the generated URL and stores the returned `storageId`. Use upload `metadata` or `ctx.storage.getMetadata(id)` for server-derived size, checksum, detected content type, image dimensions, and `createdBy`. The latter is the token identifier that requested the upload URL (empty for anonymous issuance): compare it explicitly when an application must bind finalization to that identity, because PBVex does not enforce ownership automatically. Filenames, and generic filename-derived extensions, remain client-supplied naming data. PBVex sniffs and decodes image bytes; do not trust the request MIME or filename extension.

## Choose download access explicitly

`ctx.storage.getUrl(id)` defaults to `identity`: it is short-lived, and a URL created by an authenticated function requires that same caller token. An anonymously created identity URL remains signed and needs no `Authorization` header, but possession of the URL grants access until expiry. Use `{ mode: 'capability' }` for a short-lived bearer URL suitable for `<img>` or navigation. Use `{ mode: 'public' }` only for intentionally public immutable assets; it is stable and shared-cacheable. Deletion removes the origin object, but cached public originals or variants can remain available until the configured CDN/browser cache TTL expires.

Set an image variant with the URL API because signed URLs already have query parameters:

```ts
const resized = new URL(storageUrl);
resized.searchParams.set('thumb', '320x240f');
```

Only selectors captured in the file's schema policy are accepted. Supported forms are `WxH`, `WxHt`, `WxHb`, `WxHf`, `Wx0`, and `0xH`; suffixes require two nonzero axes. Variants are generated lazily, cached as objects, and deleted with the original.

## Preserve lifecycle and limits

Image policies are snapshotted at upload. Changing `v.string()` to `v.image()` needs a document migration but does not reprocess the object. PBVex v1 has no reprocessing/backfill API: use an application-managed reupload through a schema-bound image URL, or generate a fresh original URL without `thumb` as fallback. Keep uploads within documented byte, MIME, 32-megapixel, 16,384-axis, variant-count, and output-dimension limits.

Upload happens before document attachment, so provide an authorized finalize/compensation path for abandoned uploads; active unattached objects are not inferred as garbage. Mutation deletion commits metadata state transactionally, then retries irreversible object-prefix cleanup after commit. On restart PBVex recovers leases, staged finalization, deletion, and true orphans. Monitor cleanup/data-loss logs and storage caps; never manually remove metadata rows or object prefixes. Use PocketBase backup or a stopped-process data-directory snapshot coordinated with originals/variants; preserve encryption secrets separately, then smoke-test metadata and downloads after restore.

Use `docs/guides/storage.md`, `docs/guides/image-resizing.md`, `docs/guides/limits.md`, and `docs/self-hosting.md` as authoritative references. Iterate with focused storage/schema/protocol tests; before a PR also use the complete contributor gates in `pbvex-operations`.
