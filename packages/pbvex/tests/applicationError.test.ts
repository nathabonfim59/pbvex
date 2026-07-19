import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApplicationError } from '../src/runtime/index.js';

describe('ApplicationError', () => {
  afterEach(() => {
    delete (globalThis as any).__pbvex;
  });

  it('uses a runtime-created branded instance when the runtime bridge is present', () => {
    const branded = Object.assign(new Error('conflict'), {
      name: 'ApplicationError', category: 'conflict' as const, data: { resource: 'note', retry: 1n },
    });
    const createApplicationError = vi.fn(() => branded);
    (globalThis as any).__pbvex = { createApplicationError };

    const error = new ApplicationError('conflict', { resource: 'note', retry: 1n });

    expect(error).toBe(branded);
    expect(createApplicationError).toHaveBeenCalledWith(
      'conflict', { resource: 'note', retry: 1n }, true, ApplicationError.prototype,
    );
  });

  it('is a normal Error with stable public fields outside the backend runtime', () => {
    const error = new ApplicationError('not_found');
    expect(error).toBeInstanceOf(Error);
    expect(error).toMatchObject({ name: 'ApplicationError', category: 'not_found', data: undefined });
  });
});
