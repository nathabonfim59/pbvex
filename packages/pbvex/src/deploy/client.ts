import type {
  DeploymentUploadRequest,
  DeploymentUploadResponse,
  DeploymentListResponse,
  DeploymentActivateResponse,
  DeploymentRollbackResponse,
} from '@pbvex/protocol';
import {
  isStructuredError,
  validateUploadResponse,
  validateActivateResponse,
  validateRollbackResponse,
  validateDeploymentListResponse,
} from '@pbvex/protocol';
import type { DeploymentArtifact } from '../bundler/manifest.js';
import { toUploadRequest } from '../bundler/manifest.js';

export interface DeployResult {
  ok: boolean;
  deploymentId?: string;
  status: string;
  error?: string;
}

export interface DeployClientOptions {
  url: string;
  token?: string;
  fetch?: typeof fetch;
}

export class DeployClient {
  private readonly fetch: typeof fetch;
  private readonly baseUrl: string;
  private readonly token: string | undefined;

  constructor(options: DeployClientOptions) {
    this.fetch = options.fetch ?? globalThis.fetch;
    this.baseUrl = options.url.replace(/\/+$/, '');
    this.token = options.token;
  }

  private headers(): Record<string, string> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };
    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }
    return headers;
  }

  private async parseError(response: Response): Promise<Error> {
    const text = await response.text().catch(() => 'Unknown error');
    let parsed: unknown;
    try {
      parsed = JSON.parse(text);
    } catch {
      return new Error(`Deploy failed: ${response.status} ${response.statusText} - ${text}`);
    }
    if (isStructuredError(parsed)) {
      return new Error(`[${parsed.code}] ${parsed.message}`);
    }
    return new Error(`Deploy failed: ${response.status} ${response.statusText} - ${text}`);
  }

  async upload(request: DeploymentUploadRequest): Promise<DeploymentUploadResponse> {
    const response = await this.fetch(`${this.baseUrl}/api/pbvex/deployments`, {
      method: 'POST',
      headers: this.headers(),
      body: JSON.stringify(request),
    });

    if (!response.ok) {
      throw await this.parseError(response);
    }

    return validateUploadResponse(await response.json());
  }

  async activate(deploymentId: string): Promise<DeploymentActivateResponse> {
    const response = await this.fetch(`${this.baseUrl}/api/pbvex/deployments/${encodeURIComponent(deploymentId)}/activate`, {
      method: 'POST',
      headers: this.headers(),
      body: JSON.stringify({ atomic: true }),
    });

    if (!response.ok) {
      throw await this.parseError(response);
    }

    return validateActivateResponse(await response.json());
  }

  async list(): Promise<DeploymentListResponse> {
    const response = await this.fetch(`${this.baseUrl}/api/pbvex/deployments`, {
      method: 'GET',
      headers: this.headers(),
    });

    if (!response.ok) {
      throw await this.parseError(response);
    }

    return await validateDeploymentListResponse(await response.json());
  }

  async rollback(deploymentId: string): Promise<DeploymentRollbackResponse> {
    const response = await this.fetch(`${this.baseUrl}/api/pbvex/deployments/${encodeURIComponent(deploymentId)}/rollback`, {
      method: 'POST',
      headers: this.headers(),
    });

    if (!response.ok) {
      throw await this.parseError(response);
    }

    return validateRollbackResponse(await response.json());
  }

  async deploy(artifact: DeploymentArtifact): Promise<DeployResult> {
    try {
      const request = toUploadRequest(artifact);
      const upload = await this.upload(request);
      await this.activate(upload.deploymentId);
      return { ok: true, deploymentId: upload.deploymentId, status: 'active' };
    } catch (err) {
      return { ok: false, status: 'failed', error: err instanceof Error ? err.message : String(err) };
    }
  }
}
