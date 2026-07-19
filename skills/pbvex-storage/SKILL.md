---
name: pbvex-storage
description: Implement, review, troubleshoot, or operate PBVex file and image storage, including upload URLs, StorageId fields, trusted metadata, identity/capability/public downloads, CDN caching, schema-declared thumbnails, local or S3 object storage, limits, deletion, and migration behavior.
---

# PBVex file and image storage

Store only the opaque `StorageId` in a PBVex document. Original bytes and generated thumbnails live in PocketBase's configured local or S3-compatible object storage; SQLite holds the ID, object key, status, policy, and metadata. Before upload, authorize the caller against the intended parent/resource; after attachment, authorize through the owning document before returning metadata/URLs or deleting.

A canonical `StorageId` and successful `v.image()` value validation prove only storage-ID syntax under a valid image policy descriptor. They do not prove that an object exists, contains image bytes, was uploaded through that policy, belongs to the caller/document, or may be published.

## Upload files and images

Generic files use `ctx.storage.generateUploadUrl()`. Images use a top-level schema field and a policy-bound URL:

```ts
image: v.image({
  thumbs: ['320x240f', '1600x0'],
  mimeTypes: ['image/jpeg', 'image/png', 'image/webp'],
});

const caller = await ctx.auth.getUserIdentity();
if (!caller) throw new ApplicationError('unauthorized');
await assertCanUploadPhoto(ctx, caller.tokenIdentifier);
const url = await ctx.storage.generateUploadUrl({
  table: 'photos',
  field: 'image',
});
```

Authorize before issuing either URL. `generateUploadUrl({ table, field })` resolves a top-level `v.image()` field from the active schema and puts an immutable snapshot of its `thumbs` and `mimeTypes` policy in the single-use upload claim. A generic `generateUploadUrl()` does not gain image policy merely because image bytes are uploaded.

The client POSTs bytes once and stores the returned `storageId`. PBVex inspects and decodes image bytes, enforces the snapshotted MIME/size/dimension policy, and persists trusted detected MIME, extension, dimensions, size, and checksum; do not trust the request MIME or filename extension. `createdBy` is the upload-URL caller's token identifier, or empty for anonymous issuance. It is audit data that the application must compare explicitly when needed, not automatic ownership. Filenames, and generic filename-derived extensions, remain client-supplied naming data.

## Choose download access explicitly

Authorize through the owning document before calling `getUrl`; URL mode controls delivery after application authorization, not ownership or publication policy.

| Mode | Bearer/access model | Lifetime and revocation | Cacheability | Use |
| --- | --- | --- | --- | --- |
| `identity` (default) | Authenticated issuance is bound to the same validated caller token identifier. Anonymous issuance has an empty subject: the signed URL works only on an anonymous request, with no `Authorization` header; possession supplies its short-lived access. | Origin access ends at URL expiry or object deletion; authenticated origin access also requires that caller token still be accepted. No per-URL revoke. | Authenticated: `private` through URL expiry. Anonymous: `public, max-age` through URL expiry. A cache can retain an earlier response for that max-age. | Authenticated API/fetch that can resend the caller token. Do not use as a normal `<img src>` when authenticated. |
| `capability` | Short-lived bearer URL; no caller token required, and any holder can use it. | Origin access ends at URL expiry or object deletion. No per-link revoke. | `public, max-age` through URL expiry; a cache can retain an earlier response until that max-age. | Private avatar/card/list image sources, navigation, or temporary sharing after authorization. |
| `public` | Stable unguessable bearer URL; no caller token required. | Valid until object deletion. The URL is stable across calls and signing-key rotation. | Shared/browser cacheable for configured public TTL (`public`, `s-maxage`, revalidation). Cached bytes can outlive origin deletion until TTL. | Intentionally public immutable assets and their public variants. |

## Resize for display; reserve originals

Predeclare the small set of selectors the UI needs, upload through the schema-bound policy, store the ID, and request those variants for routine rendering. Use an original URL without `thumb` only for explicit zoom/download; do not serve full-resolution originals for avatars, cards, or lists.

```ts
image: v.image({
  thumbs: ['96x96', '320x240f', '640x0'],
  mimeTypes: ['image/jpeg', 'image/png', 'image/webp'],
});

await assertCanReadPhoto(ctx, photo);
const base = await ctx.storage.getUrl(photo.image, { mode: 'capability' });
if (!base) return null;
const avatar = new URL(base);
avatar.searchParams.set('thumb', '96x96');
return avatar.toString();
```

Use `URL.searchParams`, not string concatenation, because signed identity/capability URLs already have query parameters. Selectors are PocketBase syntax: `96x96` center-crops, `320x240t`/`b` crop from top/bottom, `320x240f` fits without cropping, and `640x0`/`0x480` preserve aspect ratio. Suffixes require two nonzero axes. Only exact selectors captured at upload are accepted; there are no arbitrary or newly declared transforms for an existing object. Variants are generated lazily, stored beside the original, reused, and deleted with the original (subject to public cache TTL for already cached responses).

## Preserve lifecycle and limits

Image policies are snapshotted at upload. Changing `thumbs` or `mimeTypes` affects only later policy-bound uploads. Changing `v.string()` to `v.image()` needs a document migration, but ordinary `v.image()` value validation still checks only canonical `pbv_` StorageId syntax and the policy descriptor, not object existence, bytes, ownership, or upload provenance. Old generic uploads remain generic and cannot serve variants. PBVex v1 has no reprocessing/backfill API: application code must reupload through a schema-bound image URL, or temporarily use the original without `thumb` as a fallback. Keep uploads within documented byte, MIME, 32-megapixel, 16,384-axis, variant-count, and output-dimension limits.

Upload happens before document attachment, so provide an authorized finalize/compensation path for abandoned uploads; active unattached objects are not inferred as garbage. Mutation deletion commits metadata state transactionally, then retries irreversible object-prefix cleanup after commit. On restart PBVex recovers leases, staged finalization, deletion, and true orphans. Monitor cleanup/data-loss logs and storage caps; never manually remove metadata rows or object prefixes. Use PocketBase backup or a stopped-process data-directory snapshot coordinated with originals/variants; preserve encryption secrets separately, then smoke-test metadata and downloads after restore.

Use `docs/guides/storage.md`, `docs/guides/image-resizing.md`, `docs/guides/limits.md`, and `docs/self-hosting.md` as authoritative references. Iterate with focused storage/schema/protocol tests; before a PR also use the complete contributor gates in `pbvex-operations`.
