# Provider

`PBVexProvider` makes a `Client` available to the component tree via `PBVexContext`.

## Usage

```tsx
import { Client } from '@pbvex/client';
import { PBVexProvider } from '@pbvex/react';
import { App } from './App.js';

const client = new Client('http://localhost:8090');

export function Root() {
  return (
    <PBVexProvider client={client}>
      <App />
    </PBVexProvider>
  );
}
```

## Props

```ts
interface PBVexProviderProps {
  client: Client;
  children?: ReactNode;
}
```

## Client ownership

`PBVexProvider` does not close the client on unmount. The client is owned by the caller. Create it once per application and close it when the app is destroyed, such as in a test cleanup or a top-level `useEffect`:

```tsx
import { useEffect } from 'react';
import { Client } from '@pbvex/client';
import { PBVexProvider } from '@pbvex/react';

const client = new Client('http://localhost:8090');

export function Root() {
  useEffect(() => {
    return () => client.close();
  }, []);

  return (
    <PBVexProvider client={client}>
      <App />
    </PBVexProvider>
  );
}
```

## usePBVexClient

Access the client directly:

```tsx
import { usePBVexClient } from '@pbvex/react';

function LogoutButton() {
  const client = usePBVexClient();

  return (
    <button
      onClick={() => {
        client.clearAuth();
      }}
    >
      Log out
    </button>
  );
}
```

`usePBVexClient` throws if called outside a `PBVexProvider`.
