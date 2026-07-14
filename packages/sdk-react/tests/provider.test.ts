import { describe, it, expect, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import React from 'react';
import { renderToString } from 'react-dom/server';
import type { FunctionReference } from '@pbvex/sdk-core';
import { usePBVexClient, useQuery } from '../src/index.js';
import { createClient, createWrapper, createStrictWrapper, MockRealtimeTransport } from './helpers.js';

describe('PBVexProvider', () => {
  it('throws when usePBVexClient is used without a provider', () => {
    expect(() => renderHook(() => usePBVexClient())).toThrow(
      'usePBVexClient must be used within a PBVexProvider',
    );
  });

  it('throws when useQuery is used without a provider', () => {
    const ref = { _path: 'test:query', _type: 'query' } as FunctionReference<
      'query',
      { x: number },
      string,
      'public'
    >;
    expect(() => renderHook(() => useQuery(ref, { x: 1 }))).toThrow(
      'usePBVexClient must be used within a PBVexProvider',
    );
  });

  it('returns the client inside a provider', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const { result } = renderHook(() => usePBVexClient(), {
      wrapper: createWrapper(client),
    });
    expect(result.current).toBe(client);
  });

  it('does not close an externally owned client on unmount', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const closeSpy = vi.spyOn(client, 'close');
    const { unmount } = renderHook(() => usePBVexClient(), {
      wrapper: createWrapper(client),
    });
    unmount();
    expect(closeSpy).not.toHaveBeenCalled();
  });

  it('does not subscribe to queries during SSR', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const ref = { _path: 'test:query', _type: 'query' } as FunctionReference<
      'query',
      { x: number },
      string,
      'public'
    >;

    function Component() {
      const value = useQuery(ref, { x: 1 });
      return React.createElement('div', null, value === undefined ? 'loading' : value);
    }

    const html = renderToString(
      React.createElement(
        createWrapper(client),
        null,
        React.createElement(Component),
      ),
    );

    expect(transport.watchCount).toBe(0);
    expect(html).toContain('loading');
  });

  it('cleans up StrictMode double subscriptions on unmount', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const ref = { _path: 'test:query', _type: 'query' } as FunctionReference<
      'query',
      { x: number },
      string,
      'public'
    >;

    const { unmount } = renderHook(() => useQuery(ref, { x: 1 }), {
      wrapper: createStrictWrapper(client),
    });

    expect(transport.watchCount).toBeGreaterThan(0);
    expect(transport.unsubscribeCount).toBeLessThan(transport.watchCount);

    unmount();

    expect(transport.unsubscribeCount).toBe(transport.watchCount);
    expect(transport.connectionState).toBe('disconnected');
  });
});
