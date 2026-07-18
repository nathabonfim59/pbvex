# React app slice

A complete React app slice with provider, routing, query, mutation, and action.

## Entry point

```tsx
// main.tsx
import React from 'react';
import { createRoot } from 'react-dom/client';
import { Client } from '@pbvex/client';
import { PBVexProvider } from '@pbvex/react';
import { App } from './App.js';

const client = new Client('http://localhost:8090');

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <PBVexProvider client={client}>
      <App />
    </PBVexProvider>
  </React.StrictMode>,
);
```

## App

```tsx
// App.tsx
import { Login } from './Login.js';
import { MessageView } from './MessageView.js';
import { useEffect, useState } from 'react';
import { usePBVexClient } from '@pbvex/react';

export function App() {
  const client = usePBVexClient();
  const [userId, setUserId] = useState<string | null>(
    client.authStore.isValid ? client.authStore.record?.id ?? null : null,
  );

  useEffect(() => {
    const syncAuth = () => setUserId(
      client.authStore.isValid ? client.authStore.record?.id ?? null : null,
    );
    const unsubscribe = client.authStore.onChange(syncAuth);
    const timer = window.setInterval(syncAuth, 30_000);
    return () => {
      unsubscribe();
      window.clearInterval(timer);
    };
  }, [client]);

  return (
    <div>
      <h1>PBVex Messages</h1>
      {userId ? (
        <MessageView channel="general" />
      ) : (
        <Login onLogin={setUserId} />
      )}
    </div>
  );
}
```

## Login

```tsx
// Login.tsx
import { usePBVexClient } from '@pbvex/react';
import { useState } from 'react';

export function Login({ onLogin }: { onLogin: (userId: string) => void }) {
  const client = usePBVexClient();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const result = await client.auth
      .collection('users')
      .authWithPassword(email, password);
    onLogin(result.record.id);
  }

  return (
    <form onSubmit={handleSubmit}>
      <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
      <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
      <button type="submit">Log in</button>
    </form>
  );
}
```

## Message list

```tsx
// MessageView.tsx
import { useQuery, useMutation, useAction } from '@pbvex/react';
import { useState } from 'react';
import { api } from './pbvex/_generated/api.js';

interface Message {
  id: string;
  body: string;
  channel: string;
}

export function MessageView({ channel }: { channel: string }) {
  const messages = useQuery(api.messages.list, { channel });
  const send = useMutation(api.messages.send);
  const notify = useAction(api.messages.notify);
  const [body, setBody] = useState('');

  if (messages === undefined) return <p>Loading…</p>;

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const sent = await send({ body, channel });
    setBody('');
    await notify({ messageId: sent.id });
  }

  return (
    <div>
      <ul>
        {messages.map((message: Message) => (
          <li key={message.id}>{message.body}</li>
        ))}
      </ul>
      <form onSubmit={handleSubmit}>
        <input value={body} onChange={(e) => setBody(e.target.value)} />
        <button type="submit">Send</button>
      </form>
    </div>
  );
}
```

## Error boundary

```tsx
// ErrorFallback.tsx
export function ErrorFallback({ error }: { error: Error }) {
  return (
    <div role="alert">
      <p>Something went wrong:</p>
      <pre>{error.message}</pre>
    </div>
  );
}
```

Wrap `MessageView` with an error boundary to catch `PBVexError` thrown by `useQuery`.
