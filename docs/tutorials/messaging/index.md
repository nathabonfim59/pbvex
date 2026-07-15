# Build an instant-messaging app

This tutorial builds a small but complete messaging backend with PBVex. It combines authentication, application profiles, contacts, conversation membership, authorization, realtime messages, and file attachments into one coherent application.

The tutorial focuses on backend contracts and framework-neutral client calls. You can use the resulting generated API from the core client, React hooks, or Svelte runes.

## What you will build

By the end, the application supports:

- signing in with an application account;
- creating a PBVex profile for the authenticated identity;
- finding users by handle and adding private contacts;
- starting direct or group conversations;
- allowing only current members to read and send messages;
- allowing senders to edit their own messages;
- subscribing to message updates over realtime;
- uploading attachments through short-lived URLs;
- returning attachment download URLs only to conversation members.

## Tutorial map

1. [Data model](./data-model.md) — define every table and index used by the application.
2. [Authentication and profiles](./authentication-and-profiles.md) — sign in and connect an auth identity to an application profile.
3. [Contacts](./contacts.md) — search profiles, add contacts, and return hydrated contact cards.
4. [Conversations and permissions](./conversations.md) — create membership and enforce participant/admin rules.
5. [Messages and realtime](./messages-and-realtime.md) — send, paginate, edit, and subscribe to messages.
6. [Attachments](./attachments.md) — upload files, attach them to messages, and authorize downloads.

Each page builds on the previous one. Keep the final schema in one `pbvex/schema.ts` file even though the walkthrough introduces its tables by feature.

## Prerequisites

Complete the [Quickstart](../../quickstart.md) first. You should have:

- a running PBVex server;
- an application auth collection named `users` with at least one enabled sign-in method;
- a TypeScript project initialized with `pbvex init`;
- `pbvex` and `@pbvex/client` installed;
- a deployment token available outside source control.

The tutorial uses password authentication in short examples and works the same way after OTP, OAuth2, or MFA establishes the identity.

## Project layout

The finished backend uses this structure:

```text
pbvex/
├── schema.ts
├── profiles.ts
├── contacts.ts
├── conversations.ts
├── messages.ts
├── attachments.ts
└── lib/
    ├── identity.ts
    └── membership.ts
```

After changing schema or function exports, regenerate the typed contract:

```bash
pbvex codegen
pbvex typecheck
pbvex build --check
```

## The request flow

The application deliberately keeps authorization on the server:

```text
authenticated client
        |
        v
public PBVex function
        |
        +-- resolve identity
        +-- verify owner/membership/role
        +-- perform bounded indexed reads or transactional writes
        v
typed result or subscription update
```

The client never supplies its own owner, sender, or administrator identity. It supplies resource IDs and user input; the function derives the actor from `ctx.auth` and checks current database state.

Start with the [complete data model](./data-model.md).

After finishing the messaging backend, the standalone [FakePayment tutorial](../payments/) shows how to add checkout, webhooks, and premium attachments as an optional extension.
