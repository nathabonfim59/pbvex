# Image uploads and resizing

PBVex can validate uploaded raster images, record trusted metadata, and generate a fixed set of thumbnails on demand. Image handling is schema-driven: the schema defines which formats and thumbnail sizes are allowed, and an upload URL snapshots that policy for the resulting file.

Use this feature when your application needs image dimensions or predictable display variants. Use the generic [file storage flow](./storage.md) for other files.

## Declare an image field

Declare an owning top-level field with `v.image()`:

```ts
// pbvex/schema.ts
import { defineSchema, defineTable } from 'pbvex/server';
import { v } from 'pbvex/values';

export default defineSchema({
  photos: defineTable({
    image: v.image({
      thumbs: ['96x96', '320x240f', '1600x0'],
      mimeTypes: ['image/jpeg', 'image/png', 'image/webp'],
    }),
    caption: v.optional(v.string()),
  }),
});
```

`v.image()` stores a `StorageId`, not image bytes. Its options define the upload and resize policy:

| Option | Default | Behavior |
| --- | --- | --- |
| `thumbs` | `[]` | Exact thumbnail selectors that downloads may request. At most 16 unique entries. |
| `mimeTypes` | GIF, JPEG, PNG, and WebP | Non-empty allowlist of accepted decoded image formats. |

Supported MIME values are `image/gif`, `image/jpeg`, `image/png`, and `image/webp`. Each thumbnail dimension may be at most 4,096. A bounded `WxH` thumbnail may contain at most 16,777,216 pixels, and `0x0` is invalid. An original may contain at most 32,000,000 pixels and may not exceed 16,384 pixels on either axis.

The policy belongs to the field. Keep image fields at the top level so `generateUploadUrl` can identify them by table and field name.

## Generate a policy-bound upload URL

Pass the image field to `generateUploadUrl`:

```ts
// pbvex/photos.ts
import { mutation } from './_generated/server';

export const createImageUpload = mutation({
  handler: async (ctx) => {
    // Authorize the caller before issuing an upload URL.
    return ctx.storage.generateUploadUrl({
      table: 'photos',
      field: 'image',
    });
  },
});
```

PBVex resolves the active schema field and copies its image policy into the single-use upload token. Uploading to a generic URL from `generateUploadUrl()` does not create an image-policy file, even if the bytes happen to be an image.

Upload the file directly to the returned URL:

```ts
import type { StorageUploadResponse } from '@pbvex/client';

const uploadUrl = await client.mutation(api.photos.createImageUpload);
const response = await fetch(uploadUrl, {
  method: 'POST',
  headers: {
    'Content-Type': file.type || 'application/octet-stream',
    'X-Upload-Filename': file.name,
  },
  body: file,
});

if (!response.ok) {
  throw new Error(`Image upload failed: ${response.status}`);
}

const uploaded = await response.json() as StorageUploadResponse;
if (uploaded.metadata?.kind !== 'image') {
  throw new Error('Expected trusted image metadata');
}

await client.mutation(api.photos.create, {
  image: uploaded.storageId,
  width: uploaded.metadata.width,
  height: uploaded.metadata.height,
});
```

The request `Content-Type` and filename are hints, not proof of format. PBVex inspects the bytes, checks the detected MIME against the field policy, and verifies that the image header decodes. Invalid, unsupported, or policy-disallowed bytes fail with `invalid_content`.

## Trusted image metadata

The successful upload response includes trusted metadata:

```ts
type StorageImageMetadata = {
  storageId: StorageId;
  kind: 'image';
  createdBy: string;
  filename: string;
  contentType: string;
  extension: string;
  size: number;
  sha256: string;
  width: number;
  height: number;
  thumbs: readonly string[];
};
```

`contentType`, `extension`, `width`, and `height` come from the uploaded bytes. `size` and `sha256` describe the original object. `thumbs` is the policy captured when that object was uploaded. `createdBy` is the token identifier that requested the upload URL, or an empty string for anonymous issuance; it is trusted audit metadata for explicit application checks, not automatic storage authorization. `filename` is normalized but remains client-supplied display data; do not use it as format evidence.

Read the same persisted data later with `ctx.storage.getMetadata(storageId)`. It returns `null` for an invalid, missing, or deleted ID. As with `getUrl`, your function must authorize access before returning metadata to a caller.

```ts
export const readImage = query({
  args: { photoId: v.id('photos') },
  handler: async (ctx, { photoId }) => {
    const photo = await ctx.db.get(photoId);
    if (!photo) return null;

    // Apply the application's access rule here.
    const metadata = await ctx.storage.getMetadata(photo.image);
    return { photo, metadata };
  },
});
```

## Thumbnail selectors

PBVex uses PocketBase thumbnail syntax. The selector is case-sensitive and must exactly match an entry declared in `thumbs`:

| Selector | Result |
| --- | --- |
| `320x240` | Resize and center-crop to exactly 320 by 240. |
| `320x240t` | Resize and crop from the top to exactly 320 by 240. |
| `320x240b` | Resize and crop from the bottom to exactly 320 by 240. |
| `320x240f` | Fit within 320 by 240 without cropping, preserving aspect ratio. |
| `320x0` | Set width to 320 and calculate height from the aspect ratio. |
| `0x240` | Set height to 240 and calculate width from the aspect ratio. |

The suffix applies only when both dimensions are nonzero. For `Wx0` and `0xH`, PBVex checks the derived dimension against the same 4,096-axis and 16,777,216-pixel output limits when the original is uploaded. An image whose extreme aspect ratio would produce an oversized declared variant is rejected. Declare the exact output shapes your UI uses instead of accepting client-selected dimensions. This bounds storage and processing work and prevents the download endpoint from becoming an arbitrary image transformation service.

## Build a thumbnail URL

Get a normal storage URL, then set its `thumb` query parameter:

```ts
function withThumb(storageUrl: string, thumb: string): string {
  const url = new URL(storageUrl);
  url.searchParams.set('thumb', thumb);
  return url.toString();
}

const originalUrl = await ctx.storage.getUrl(photo.image, {
  mode: 'capability',
});

return originalUrl ? withThumb(originalUrl, '320x240f') : null;
```

Use `URL.searchParams` because identity and capability URLs already contain signature parameters. A public URL has no existing query string, but the same helper works for all three modes:

```ts
const publicUrl = await ctx.storage.getUrl(photo.image, { mode: 'public' });
const thumbnailUrl = publicUrl && withThumb(publicUrl, '96x96');
```

The URL mode keeps its normal authorization and cache behavior:

| Mode | Thumbnail access |
| --- | --- |
| `identity` (default) | Short-lived and requires the same caller bearer token. Usually unsuitable as a plain browser image `src`. |
| `capability` | Short-lived bearer URL suitable for an image `src`. Anyone with the URL can access it until expiry. |
| `public` | Stable bearer URL with shared-cache headers. Use only for intentionally public images. |

Adding or changing `thumb` does not bypass authorization. The server first validates the signed or public URL, then checks the selector against the immutable thumbnail policy stored with the image.

## Generation, caching, and deletion

Variants are generated lazily. The first request for a declared selector reads the original, applies orientation, resizes it with PocketBase's image implementation, and stores the result beside the original in the configured local or S3-compatible filesystem. Later requests reuse that object.

Concurrent requests for the same missing variant are deduplicated within one server process. Full upload inspection and variant generation share a two-worker decode limit per server process. Multiple server processes can still race to create the same object, so the shared object store must provide safe replacement semantics. Animated images use their first frame for validation and thumbnails.

Each variant has its own size, content type, modification time, and ETag. It does not return the original object's `Digest` header because the bytes differ. GET, HEAD, conditional requests, and ranges otherwise behave like original-file downloads.

Deleting the `StorageId` deletes the original and every generated variant. Public CDN or browser copies can remain available until the configured public cache TTL expires; see [File storage](./storage.md#download-behavior-and-backends).

## Schema changes and existing files

The policy stored with an uploaded image is immutable. Changing `thumbs` or `mimeTypes` affects newly generated upload URLs and files uploaded through them; it does not rewrite metadata for existing objects.

Changing an existing document field from `v.string()` to `v.image()` validates that its value has `StorageId` syntax, but it does not inspect or reprocess the referenced object. Files previously uploaded through a generic upload URL remain `kind: 'file'` and cannot serve `?thumb=` variants. Reupload or explicitly reprocess those originals through an image-policy upload flow if they need resizing.

Application code can provide a safe fallback during migration:

```ts
const metadata = await ctx.storage.getMetadata(photo.image);
const original = await ctx.storage.getUrl(photo.image, { mode: 'public' });

return {
  ...photo,
  imageUrl:
    original && metadata?.kind === 'image' && metadata.thumbs.includes('320x240f')
      ? withThumb(original, '320x240f')
      : original,
};
```

## Failure cases

| Symptom | Cause |
| --- | --- |
| Upload returns `invalid_content` | Bytes are not a decodable supported image, or their detected MIME is not allowed by the field. |
| Thumbnail returns 404 | The file is not an image-policy upload, or the exact selector was not captured in its `thumbs` metadata. |
| Thumbnail URL fails authorization | The base identity/capability URL expired, its signature changed, or an identity-bound request omitted the caller token. |
| A newly declared selector returns 404 for old images | Existing image objects retain the policy from upload time. Reupload or reprocess them. |
| First thumbnail request is slower | The variant is generated lazily and persisted; subsequent requests reuse it. |

There is no arbitrary resize endpoint, eager variant generation API, or automatic backfill in v1. Choose a small set of variants based on actual UI layouts, keep originals within sensible upload and pixel dimensions, and use public mode only when the source image and every declared variant may be publicly cached.
