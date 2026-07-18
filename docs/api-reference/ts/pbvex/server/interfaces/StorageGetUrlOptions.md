[pbvex](../../index.md) / [server](../index.md) / StorageGetUrlOptions

# Interface: StorageGetUrlOptions

## Properties

### mode

> **mode**: `"public"` \| `"identity"` \| `"capability"`

Identity-bound URLs are the default and require the caller's bearer token.
Capability URLs authorize anyone possessing the short-lived signed URL.
Public URLs are stable, CDN-cacheable bearer URLs that remain valid until deletion.
