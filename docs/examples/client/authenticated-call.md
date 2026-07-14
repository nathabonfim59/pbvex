# Authenticated call example

PBVex uses PocketBase record tokens passed as `Authorization: Bearer <token>`. The client supports static tokens, async providers, and per-call overrides.

## Token provider

```ts
import { Client, PBVexError } from '@pbvex/sdk-core';
import { api } from './pbvex/_generated/api.js';

async function getToken(): Promise<string | undefined> {
  // Replace with your auth store
  const session = localStorage.getItem('pb_token');
  return session ?? undefined;
}

const client = new Client('http://localhost:8090', {
  auth: getToken,
});

async function loadProfile() {
  try {
    const profile = await client.query(api.users.getSelf);
    return profile;
  } catch (error) {
    if (error instanceof PBVexError && error.code === 'unauthorized') {
      console.error('Not authenticated');
    }
    throw error;
  }
}
```

## Update auth on login/logout

```ts
function login(token: string) {
  localStorage.setItem('pb_token', token);
  client.setAuth(token);
}

function logout() {
  localStorage.removeItem('pb_token');
  client.clearAuth();
}
```

`setAuth` and `clearAuth` also refresh live realtime subscriptions.

## Per-call override

```ts
const adminOnly = await client.query(api.admin.stats, undefined, {
  auth: 'record-token-for-this-call',
});
```

For queries with optional args, `undefined` is passed as the args slot.

## Token refresh

A provider can read a refreshed token from your auth store each call:

```ts
const client = new Client('http://localhost:8090', {
  auth: async () => {
    const token = await refreshTokenIfNeeded();
    return token;
  },
});
```
