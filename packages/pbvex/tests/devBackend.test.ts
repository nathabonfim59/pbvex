import { describe, expect, it } from 'vitest';
import path from 'node:path';
import { applicationPocketBaseMigrationsDir, managedBackendArgs } from '../src/dev/backend.js';

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

  it('passes the application migrations directory to the backend', () => {
    expect(managedBackendArgs('/tmp/data', '127.0.0.1:8090', {
      pocketbaseMigrationsDir: '/workspace/pbvex/pocketbaseMigrations',
    })).toContain('/workspace/pbvex/pocketbaseMigrations');
  });

  it('sets the configured origin for generated storage URLs', () => {
    expect(managedBackendArgs('/tmp/data', '127.0.0.1:8084', {
      storageBaseUrl: 'http://127.0.0.1:8084',
    })).toContain('http://127.0.0.1:8084');
  });

  it('uses only pbvex/pocketbaseMigrations by default and resolves an explicit override', () => {
    expect(applicationPocketBaseMigrationsDir('/workspace')).toBe(path.join('/workspace', 'pbvex', 'pocketbaseMigrations'));
    expect(applicationPocketBaseMigrationsDir('/workspace', 'custom/migrations')).toBe(
      path.join('/workspace', 'custom', 'migrations'),
    );
  });
});
