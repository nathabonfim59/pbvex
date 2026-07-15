import { vi } from 'vitest';
import React from 'react';
import { Client } from '@pbvex/client';
import type {
  ConnectionState,
  QueryResult,
  RealtimeTransport,
  Unsubscribe,
  WatchOptions,
} from '@pbvex/client';
import { PBVexProvider } from '../src/provider.js';

export interface WatchCall {
  path: string;
  args: unknown;
  options: WatchOptions<unknown>;
  unsubscribe: () => void;
}

export class MockRealtimeTransport implements RealtimeTransport {
  connectionState: ConnectionState = 'disconnected';
  watchCount = 0;
  unsubscribeCount = 0;
  watchCalls: WatchCall[] = [];
  private watchers = new Set<WatchOptions<unknown>>();

  watch<Args, Return>(path: string, args: Args, options: WatchOptions<Return>): Unsubscribe {
    this.watchCount += 1;
    this.watchers.add(options as WatchOptions<unknown>);
    this.connectionState = 'connected';
    options.onUpdate({ data: undefined, error: null, isLoading: true } as QueryResult<Return>);
    const unsubscribe = () => {
      this.watchers.delete(options as WatchOptions<unknown>);
      this.unsubscribeCount += 1;
      if (this.watchers.size === 0) {
        this.connectionState = 'disconnected';
      }
    };
    this.watchCalls.push({
      path,
      args,
      options: options as WatchOptions<unknown>,
      unsubscribe,
    });
    return unsubscribe;
  }

  trigger(data: unknown): void {
    for (const watcher of this.watchers) {
      watcher.onUpdate({ data, error: null, isLoading: false } as QueryResult<unknown>);
    }
  }

  triggerError(error: Error): void {
    for (const watcher of this.watchers) {
      watcher.onError?.(error);
    }
  }

  close(): void {
    this.watchers.clear();
    this.connectionState = 'disconnected';
  }
}

export function createClient(transport: MockRealtimeTransport): Client {
  return new Client('http://localhost:8090', {
    fetch: vi.fn() as unknown as typeof fetch,
    realtimeTransport: transport,
  });
}

export function createWrapper(client: Client) {
  return function Wrapper({ children }: { children?: React.ReactNode }) {
    return React.createElement(PBVexProvider, { client }, children);
  };
}

export function createStrictWrapper(client: Client) {
  return function StrictWrapper({ children }: { children?: React.ReactNode }) {
    return React.createElement(
      React.StrictMode,
      null,
      React.createElement(PBVexProvider, { client }, children),
    );
  };
}
