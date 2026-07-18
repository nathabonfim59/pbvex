# Message attachments

Attachment upload is a three-step flow: an authenticated function records permission to attach one caller-owned object to a message and creates a short-lived upload URL, the browser uploads bytes directly and receives an opaque storage ID, then the same user finalizes that intent with server-derived metadata.

## Generate an upload URL

The base tutorial allows each message sender to upload for that message. The separate [FakePayment tutorial](../payments/) shows how to make attachment uploads a premium extension.

```ts
// pbvex/attachments.ts
import { mutation, query } from './_generated/server';
import type { StorageId } from 'pbvex/server';
import { v } from 'pbvex/values';
import { requireIdentity } from './lib/identity';
import { requireMembership } from './lib/membership';

export const createUpload = mutation({
  args: { messageId: v.id('messages') },
  returns: v.object({
    uploadIntentId: v.id('attachmentUploads'),
    uploadUrl: v.string(),
  }),
  handler: async (ctx, { messageId }) => {
    const user = await requireIdentity(ctx.auth);
    const message = await ctx.db.get(messageId);
    if (!message || message.sender !== user.tokenIdentifier) {
      throw new Error('forbidden');
    }
    await requireMembership(ctx, message.conversationId, user.tokenIdentifier);

    const uploadIntentId = await ctx.db.insert('attachmentUploads', {
      messageId,
      owner: user.tokenIdentifier,
    });
    const uploadUrl = await ctx.storage.generateUploadUrl();
    return { uploadIntentId, uploadUrl };
  },
});
```

The URL expires and can be used only once. Request a new URL for each retry or file.

## Upload from the browser

```ts
import type { StorageUploadResponse } from '@pbvex/client';
import { api } from './pbvex/_generated/api';
import type { Id } from './pbvex/_generated/dataModel';

async function uploadAttachment(messageId: Id<'messages'>, file: File) {
  const { uploadIntentId, uploadUrl } = await client.mutation(
    api.attachments.createUpload,
    { messageId },
  );

  const response = await fetch(uploadUrl, {
    method: 'POST',
    headers: {
      'Content-Type': file.type || 'application/octet-stream',
      'X-Upload-Filename': file.name,
    },
    body: file,
  });

  if (!response.ok) {
    throw new Error(`Upload failed: ${response.status}`);
  }

  const uploaded = await response.json() as StorageUploadResponse;
  if (!uploaded.metadata) throw new Error('Upload metadata missing');
  return { uploadIntentId, uploaded };
}
```

The response metadata includes server-derived size, checksum, and detected content type. Its filename remains client-supplied display data. Registration below reads metadata again on the server so a client cannot substitute authoritative values.

## Attach the upload to your message

Only the original sender, while still a conversation member, may attach metadata to a message:

```ts
export const attach = mutation({
  args: {
    uploadIntentId: v.id('attachmentUploads'),
    messageId: v.id('messages'),
    storageId: v.string(),
  },
  returns: v.id('messageAttachments'),
  handler: async (ctx, args) => {
    const user = await requireIdentity(ctx.auth);
    const intent = await ctx.db.get(args.uploadIntentId);
    const message = await ctx.db.get(args.messageId);

    if (
      !intent ||
      intent.owner !== user.tokenIdentifier ||
      intent.messageId !== args.messageId ||
      !message ||
      message.sender !== user.tokenIdentifier
    ) {
      throw new Error('forbidden');
    }

    await requireMembership(
      ctx,
      message.conversationId,
      user.tokenIdentifier,
    );

    const metadata = await ctx.storage.getMetadata(
      args.storageId as StorageId,
    );
    if (!metadata) throw new Error('upload not found');
    if (metadata.createdBy !== user.tokenIdentifier) {
      throw new Error('upload owner mismatch');
    }

    const alreadyAttached = await ctx.db
      .query('messageAttachments')
      .withIndex('by_storage', (q) => q.eq('storageId', args.storageId))
      .unique();
    if (alreadyAttached) throw new Error('upload already attached');

    const existing = await ctx.db
      .query('messageAttachments')
      .withIndex('by_message', (q) =>
        q.eq('messageId', args.messageId),
      )
      .take(10);
    if (existing.length >= 10) throw new Error('too many attachments');

    const attachmentId = await ctx.db.insert('messageAttachments', {
      messageId: args.messageId,
      owner: user.tokenIdentifier,
      storageId: args.storageId,
      filename: metadata.filename.slice(0, 255),
      contentType: metadata.contentType.slice(0, 255),
      size: metadata.size,
    });
    await ctx.db.delete(args.uploadIntentId);
    return attachmentId;
  },
});
```

The upload intent authorizes one finalization of a caller-owned object for a specific message; deleting it in the same mutation prevents reuse. It is deliberately not an identifier for one particular upload URL: a user may choose another object whose upload URL they requested. `metadata.createdBy` is trusted audit metadata captured at upload-URL issuance, and this application explicitly compares it with the finalizing identity. PBVex does not enforce ownership automatically, so possession of a storage ID alone remains insufficient. The `by_storage` check prevents one object from being registered as multiple message attachments.

Client flow:

```ts
const { uploadIntentId, uploaded } = await uploadAttachment(messageId, file);

await client.mutation(api.attachments.attach, {
  uploadIntentId,
  messageId,
  storageId: uploaded.storageId,
});
```

For a UI that sends text and files together, create the message first, upload and attach each file, and surface partial failure so the user can retry attachment without silently duplicating the message.

## List authorized attachment metadata

Start from the message, verify conversation membership, then query its attachment range:

```ts
export const listForMessage = query({
  args: { messageId: v.id('messages') },
  handler: async (ctx, { messageId }) => {
    const user = await requireIdentity(ctx.auth);
    const message = await ctx.db.get(messageId);
    if (!message) throw new Error('not found');

    await requireMembership(
      ctx,
      message.conversationId,
      user.tokenIdentifier,
    );

    const attachments = await ctx.db
      .query('messageAttachments')
      .withIndex('by_message', (q) => q.eq('messageId', messageId))
      .take(10);

    return attachments.map(({ storageId: _storageId, owner: _owner, ...safe }) => safe);
  },
});
```

The response intentionally omits internal ownership data and the raw storage ID.

## Request an authorized download URL

Signed download URLs are short-lived. Resolve authorization every time the client needs one:

```ts
export const getDownloadUrl = query({
  args: { attachmentId: v.id('messageAttachments') },
  returns: v.union(v.string(), v.null()),
  handler: async (ctx, { attachmentId }) => {
    const user = await requireIdentity(ctx.auth);
    const attachment = await ctx.db.get(attachmentId);
    if (!attachment) return null;

    const message = await ctx.db.get(attachment.messageId);
    if (!message) return null;

    await requireMembership(
      ctx,
      message.conversationId,
      user.tokenIdentifier,
    );

    return ctx.storage.getUrl(
      attachment.storageId as StorageId,
      { mode: 'capability' },
    );
  },
});
```

`ctx.storage.getUrl` does not decide application ownership. The membership check before it is the authorization boundary. Capability mode lets a native link or media element use the URL without an authorization header, so anyone who obtains the URL can download the attachment until it expires.

## Delete an attachment

Only the attachment owner should remove it in this application:

```ts
export const remove = mutation({
  args: { attachmentId: v.id('messageAttachments') },
  returns: v.boolean(),
  handler: async (ctx, { attachmentId }) => {
    const user = await requireIdentity(ctx.auth);
    const attachment = await ctx.db.get(attachmentId);

    if (!attachment || attachment.owner !== user.tokenIdentifier) {
      return false;
    }

    const message = await ctx.db.get(attachment.messageId);
    if (!message) return false;
    await requireMembership(
      ctx,
      message.conversationId,
      user.tokenIdentifier,
    );

    await ctx.db.delete(attachmentId);
    return true;
  },
});
```

This removes the message association but deliberately does not delete the storage object: `createdBy` proves who requested its upload URL, not that no profile or other application record references it. Delete bytes only when the application maintains a dedicated-object or reference-count invariant and has verified that no references remain. `ctx.storage.delete` participates in a mutation's transaction, with irreversible object cleanup after commit and durable recovery.

## Production considerations

- Enforce server upload size and MIME allowlists appropriate to messaging.
- Limit attachments per message and metadata lengths.
- Treat filenames as untrusted display values. Server-derived detected content type and size are trusted descriptions of accepted bytes, not authorization or malware-safety claims.
- Define retention for abandoned upload intents and unattached objects. If cleanup must delete uploaded objects, record enough application state to identify them without exposing raw storage IDs to clients that are not authorized to finalize the upload.
- Do not persist signed download URLs; request a fresh URL when needed.
- Define cleanup for uploaded objects that never become attached.
- Consider malware scanning before making uploads available to other members.
- Back up database metadata and object storage together.

You now have the complete messaging tutorial. Continue with the optional [FakePayment extension](../payments/), return to the [tutorial overview](./), or read the standalone [Authorization](../../guides/authorization.md), [Storage](../../guides/storage.md), and [Realtime](../../guides/client/realtime.md) reference guides.
