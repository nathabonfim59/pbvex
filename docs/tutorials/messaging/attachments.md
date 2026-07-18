# Message attachments

Attachment upload is a two-step flow: an authenticated function creates a short-lived upload URL, then the browser uploads bytes directly and receives an opaque storage ID. The application stores authorized metadata that connects that object to a message.

## Generate an upload URL

The base tutorial allows every member to upload. The separate [FakePayment tutorial](../payments/) shows how to make attachment uploads a premium extension.

```ts
// pbvex/attachments.ts
import { mutation, query } from './_generated/server';
import type { StorageId } from 'pbvex/server';
import { v } from 'pbvex/values';
import { requireIdentity } from './lib/identity';
import { requireMembership } from './lib/membership';

export const createUpload = mutation({
  args: {},
  returns: v.string(),
  handler: async (ctx) => {
    await requireIdentity(ctx.auth);
    return ctx.storage.generateUploadUrl();
  },
});
```

The URL expires and can be used only once. Request a new URL for each retry or file.

## Upload from the browser

```ts
import type { StorageUploadResponse } from '@pbvex/client';

async function uploadAttachment(file: File) {
  const uploadUrl = await client.mutation(api.attachments.createUpload);

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

  const { storageId } = await response.json() as StorageUploadResponse;
  return {
    storageId,
    filename: file.name,
    contentType: file.type || 'application/octet-stream',
    size: file.size,
  };
}
```

The server enforces the actual file-size and content-type limits. Client metadata is useful for display but is not proof of the uploaded object’s contents.

## Attach the upload to your message

Only the original sender, while still a conversation member, may attach metadata to a message:

```ts
export const attach = mutation({
  args: {
    messageId: v.id('messages'),
    storageId: v.string(),
    filename: v.string(),
    contentType: v.string(),
    size: v.number(),
  },
  returns: v.id('messageAttachments'),
  handler: async (ctx, args) => {
    const user = await requireIdentity(ctx.auth);
    const message = await ctx.db.get(args.messageId);

    if (!message || message.sender !== user.tokenIdentifier) {
      throw new Error('forbidden');
    }

    await requireMembership(
      ctx,
      message.conversationId,
      user.tokenIdentifier,
    );

    const existing = await ctx.db
      .query('messageAttachments')
      .withIndex('by_message', (q) =>
        q.eq('messageId', args.messageId),
      )
      .take(10);
    if (existing.length >= 10) throw new Error('too many attachments');

    return ctx.db.insert('messageAttachments', {
      messageId: args.messageId,
      owner: user.tokenIdentifier,
      storageId: args.storageId,
      filename: args.filename.slice(0, 255),
      contentType: args.contentType.slice(0, 255),
      size: args.size,
    });
  },
});
```

The storage ID is opaque; pass it through without parsing or constructing it. Register uploads promptly and do not expose raw storage IDs outside authorized view models.

Client flow:

```ts
const uploaded = await uploadAttachment(file);

await client.mutation(api.attachments.attach, {
  messageId,
  ...uploaded,
});
```

For a UI that sends text and files together, upload files first, create the message, attach each uploaded object, and surface partial failure so the user can retry registration without silently duplicating the message.

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

    await ctx.storage.delete(attachment.storageId as StorageId);
    await ctx.db.delete(attachmentId);
    return true;
  },
});
```

Deleting storage metadata inside a mutation participates in its transaction; irreversible object cleanup occurs after commit with durable recovery.

## Production considerations

- Enforce server upload size and MIME allowlists appropriate to messaging.
- Limit attachments per message and metadata lengths.
- Treat filenames and content types as untrusted display values.
- Do not persist signed download URLs; request a fresh URL when needed.
- Define cleanup for uploaded objects that never become attached.
- Consider malware scanning before making uploads available to other members.
- Back up database metadata and object storage together.

You now have the complete messaging tutorial. Continue with the optional [FakePayment extension](../payments/), return to the [tutorial overview](./), or read the standalone [Authorization](../../guides/authorization.md), [Storage](../../guides/storage.md), and [Realtime](../../guides/client/realtime.md) reference guides.
