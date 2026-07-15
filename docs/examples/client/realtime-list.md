# Realtime list example

A vanilla TypeScript component that renders a live message list.

```ts
import { Client, PBVexError, type QueryResult, type ConnectionState } from '@pbvex/client';
import { api } from './pbvex/_generated/api.js';

const client = new Client('http://localhost:8090');

interface Message {
  id: string;
  body: string;
  channel: string;
}

class MessageList {
  private messages: Message[] = [];
  private error: Error | null = null;
  private isLoading = true;
  private state: ConnectionState = 'disconnected';
  private unsubscribe: (() => void) | null = null;

  mount(channel: string, container: HTMLElement) {
    this.unsubscribe = client.watch(
      api.messages.list,
      { channel },
      {
        onUpdate: (result: QueryResult<Message[]>) => {
          this.isLoading = result.isLoading;
          this.error = result.error;
          this.messages = result.data ?? [];
          this.render(container);
        },
        onError: (error: Error) => {
          this.error = error;
          this.render(container);
        },
        onConnectionStateChange: (state) => {
          this.state = state;
          this.render(container);
        },
        maxReconnects: 10,
        initialReconnectDelayMs: 500,
        maxReconnectDelayMs: 30000,
      },
    );
  }

  unmount() {
    this.unsubscribe?.();
    this.unsubscribe = null;
  }

  private render(container: HTMLElement) {
    if (this.isLoading) {
      container.innerHTML = '<p>Loading…</p>';
      return;
    }

    if (this.error) {
      const message = this.error instanceof PBVexError
        ? `${this.error.code}: ${this.error.message}`
        : this.error.message;
      container.innerHTML = `<p class="error">${message}</p>`;
      return;
    }

    const header = `<p>Connection: ${this.state}</p>`;
    const items = this.messages.map((m) => `<li>${m.body}</li>`).join('');
    container.innerHTML = `${header}<ul>${items}</ul>`;
  }
}

const list = new MessageList();
const container = document.getElementById('messages')!;
list.mount('general', container);

window.addEventListener('beforeunload', () => {
  list.unmount();
  client.close();
});
```

## Key points

- `onUpdate` receives `QueryResult<Message[]>` with `isLoading`, `error`, and `data`.
- `onConnectionStateChange` reports `connecting`, `connected`, `reconnecting`, or `disconnected`.
- `unsubscribe()` tears down the SSE connection when the last watcher leaves.
- `client.close()` is called on page unload to release all connections.
