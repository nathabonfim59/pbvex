import { describe, expect, it } from 'vitest';
import { managedBackendArgs } from '../src/dev/backend.js';

describe('managed development backend arguments', () => {
  it('enables the admin UI by default after the serve subcommand', () => {
    expect(managedBackendArgs('/tmp/data', '127.0.0.1:8090')).toEqual([
      '--dir',
      '/tmp/data',
      '--hooksWatch=false',
      'serve',
      '--admin-ui',
      '--http',
      '127.0.0.1:8090',
    ]);
  });

  it('can disable the admin UI while retaining debug logging', () => {
    expect(managedBackendArgs('/tmp/data', '127.0.0.1:8090', { adminUI: false, debug: true })).toEqual([
      '--dir',
      '/tmp/data',
      '--hooksWatch=false',
      '--dev=true',
      'serve',
      '--http',
      '127.0.0.1:8090',
    ]);
  });
});
