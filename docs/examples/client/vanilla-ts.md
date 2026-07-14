# Vanilla TypeScript example

A minimal browser/Node script using `@pbvex/sdk-core`.

```ts
import { Client, PBVexClient, PBVexError } from '@pbvex/sdk-core';
import { api } from './pbvex/_generated/api.js';

const client = new PBVexClient('http://localhost:8090');

async function main() {
  try {
    // Ping a no-args query
    const ok = await client.query(api.health.ping);
    console.log('ping', ok);

    // Send a mutation
    const sent = await client.mutation(api.messages.send, {
      channel: 'general',
      body: 'hello from the client',
    });
    console.log('sent', sent);

    // Run an action
    const notified = await client.action(api.messages.notify, { messageId: sent.id });
    console.log('notified', notified);
  } catch (error) {
    if (error instanceof PBVexError) {
      console.error('pbvex error', error.code, error.message);
    } else {
      console.error('error', error);
    }
  } finally {
    client.close();
  }
}

main();
```

## String-path fallback

For dynamic paths or pre-typed code, use string paths:

```ts
const result = await client.query('messages:list', { channel: 'general' });
const created = await client.mutation('messages:send', { body: 'hello' });
```

## Run with tsx

```bash
npx tsx vanilla.ts
```
