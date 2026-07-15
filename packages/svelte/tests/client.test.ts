import { describe, it, expect, vi, beforeEach } from 'vitest';
import { Client } from '@pbvex/client';
import { getClient, setClient } from '../src/client.js';
import { MockClient } from './mockClient.js';

vi.mock('svelte', () => ({
  getContext: vi.fn(),
  setContext: vi.fn(),
}));

import { getContext, setContext } from 'svelte';

describe('client context', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('setClient stores the client in Svelte context', () => {
    const client = new MockClient();
    const result = setClient(client);
    expect(result).toBe(client);
    expect(setContext).toHaveBeenCalledTimes(1);
  });

  it('getClient returns the client from Svelte context', () => {
    const client = new MockClient();
    (getContext as ReturnType<typeof vi.fn>).mockReturnValue(client);
    expect(getClient()).toBe(client);
    expect(getContext).toHaveBeenCalledTimes(1);
  });

  it('getClient throws when no client is in context', () => {
    (getContext as ReturnType<typeof vi.fn>).mockReturnValue(undefined);
    expect(() => getClient()).toThrow('No PBVex client found in Svelte context');
  });

});
