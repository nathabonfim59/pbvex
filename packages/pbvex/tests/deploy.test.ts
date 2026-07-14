import { describe, it, expect } from 'vitest';
import { DeployClient } from '../src/deploy/client.js';
import type { DeploymentArtifact } from '../src/bundler/manifest.js';

const ISO_NOW = '2024-01-01T00:00:00Z';
const VALID_DEPLOYMENT_ID = 'dep_1';

function createArtifact(bundleBase64 = 'Y29uc29sZS5sb2coImhlbGxvIik7'): DeploymentArtifact {
  return {
    project: 'test',
    target: 'local',
    manifest: {
      protocolVersion: 'v1',
      deploymentId: 'test',
      functions: [],
      schema: { tables: [] },
    },
    bundle: bundleBase64,
    sha256: '3781f94ea812bb33437de9049e04bc3af41a0e7397164b057379c08c3b0ac489',
    size: 21,
    modules: [],
  };
}

function createUploadResponse(bundleHash: string) {
  return {
    deploymentId: VALID_DEPLOYMENT_ID,
    bundleHash,
    acceptedAt: ISO_NOW,
  };
}

function createActivateResponse() {
  return {
    deploymentId: VALID_DEPLOYMENT_ID,
    activatedAt: ISO_NOW,
  };
}

function createRollbackResponse() {
  return {
    deploymentId: VALID_DEPLOYMENT_ID,
    rolledBackAt: ISO_NOW,
  };
}

describe('deploy client', () => {
  it('uploads an artifact to the deployments endpoint as a DeploymentUploadRequest', async () => {
    const captured: { url: string; body: string; headers: Record<string, string> }[] = [];
    const artifact = createArtifact();
    const mockFetch = async (url: string, init: { body?: BodyInit; headers?: Record<string, string> }) => {
      if (init.body) {
        captured.push({ url, body: init.body as string, headers: init.headers ?? {} });
      }
      if (url.endsWith('/activate')) {
        return { ok: true, status: 200, json: async () => createActivateResponse() } as Response;
      }
      return { ok: true, status: 200, json: async () => createUploadResponse(artifact.sha256) } as Response;
    };

    const client = new DeployClient({ url: 'http://localhost:8090', token: 'test-token', fetch: mockFetch as any } as any);
    const result = await client.deploy(artifact);
    expect(result.ok).toBe(true);
    expect(result.deploymentId).toBe(VALID_DEPLOYMENT_ID);
    const upload = captured.find((c) => c.url === 'http://localhost:8090/api/pbvex/deployments');
    expect(upload).toBeDefined();
    expect(upload!.headers['Authorization']).toBe('Bearer test-token');
    const parsedBody = JSON.parse(upload!.body);
    expect(parsedBody).toMatchObject({ manifest: artifact.manifest, bundle: artifact.bundle, sha256: artifact.sha256, size: artifact.size });
  });

  it('activates with atomic: true', async () => {
    const captured: { url: string; body: string }[] = [];
    const artifact = createArtifact();
    const mockFetch = async (url: string, init: { body?: BodyInit; headers?: Record<string, string> }) => {
      if (init.body) {
        captured.push({ url, body: init.body as string });
      }
      if (url.endsWith('/activate')) {
        return { ok: true, status: 200, json: async () => createActivateResponse() } as Response;
      }
      return { ok: true, status: 200, json: async () => createUploadResponse(artifact.sha256) } as Response;
    };

    const client = new DeployClient({ url: 'http://localhost:8090', token: 'test-token', fetch: mockFetch as any } as any);
    await client.deploy(artifact);
    const activate = captured.find((c) => c.url.includes('/activate'));
    expect(activate).toBeDefined();
    expect(JSON.parse(activate!.body)).toEqual({ atomic: true });
  });

  it('returns an error if the server responds with a structured error', async () => {
    const mockFetch = async () =>
      ({
        ok: false,
        status: 400,
        text: async () => JSON.stringify({ error: true, code: 'invalid_manifest', message: 'bad manifest' }),
      } as Response);
    const client = new DeployClient({ url: 'http://localhost:8090', fetch: mockFetch as any } as any);
    const result = await client.deploy(createArtifact());
    expect(result.ok).toBe(false);
    expect(result.error).toContain('invalid_manifest');
    expect(result.error).toContain('bad manifest');
  });

  it('lists deployments', async () => {
    const mockFetch = async (url: string) => {
      expect(url).toBe('http://localhost:8090/api/pbvex/deployments');
      return { ok: true, status: 200, json: async () => ({ deployments: [] }) } as Response;
    };
    const client = new DeployClient({ url: 'http://localhost:8090', fetch: mockFetch as any } as any);
    const list = await client.list();
    expect(list.deployments).toEqual([]);
  });

  it('rolls back a deployment', async () => {
    const mockFetch = async (url: string) => {
      expect(url).toBe('http://localhost:8090/api/pbvex/deployments/dep_1/rollback');
      return { ok: true, status: 200, json: async () => createRollbackResponse() } as Response;
    };
    const client = new DeployClient({ url: 'http://localhost:8090', fetch: mockFetch as any } as any);
    const result = await client.rollback(VALID_DEPLOYMENT_ID);
    expect(result.deploymentId).toBe(VALID_DEPLOYMENT_ID);
  });
});
