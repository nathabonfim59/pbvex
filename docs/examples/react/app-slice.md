# React app slice

A complete React app slice with provider, routing, query, mutation, and action.

## Entry point

```tsx
// main.tsx
import React from 'react';
import { createRoot } from 'react-dom/client';
import { Client } from '@pbvex/sdk-core';
import { PBVexProvider } from '@pbvex/sdk-react';
import { App } from './App.js';

const client = new Client('http://localhost:8090');

const token = localStorage.getItem('pb_token');
if (token) client.setAuth(token);

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
import { useState } from 'react';

export function App() {
  const [userId, setUserId] = useState<string | null>(null);

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
import { usePBVexClient } from '@pbvex/sdk-react';
import { useState } from 'react';
import { api } from './pbvex/_generated/api.js';

export function Login({ onLogin }: { onLogin: (userId: string) => void }) {
  const client = usePBVexClient();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const { token, userId } = await client.mutation(api.auth.login, { email, password });
    localStorage.setItem('pb_token', token);
    client.setAuth(token);
    onLogin(userId);
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
import { useQuery, useMutation, useAction } from '@pbvex/sdk-react';
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
