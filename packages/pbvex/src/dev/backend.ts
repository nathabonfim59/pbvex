import { randomBytes } from 'node:crypto';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import type { ChildProcess } from 'node:child_process';
import { spawnServer } from '@pbvex/server';
import type { ResolvedConfig } from '../config/config.js';

export interface ManagedBackend {
  token: string;
  dataDir: string;
  close: () => Promise<void>;
}

export interface ManagedBackendOptions {
  debug?: boolean;
  adminUI?: boolean;
}

export function managedBackendArgs(
  dataDir: string,
  address: string,
  options: ManagedBackendOptions = {},
): string[] {
  const args = ['--dir', dataDir, '--hooksWatch=false'];
  if (options.debug) args.push('--dev=true');
  args.push('serve');
  if (options.adminUI !== false) args.push('--admin-ui');
  args.push('--http', address);
  return args;
}

function isLoopbackHostname(hostname: string): boolean {
  const normalized = hostname.toLowerCase();
  return normalized === 'localhost' || normalized === '127.0.0.1' || normalized === '[::1]' || normalized === '::1';
}

function localListenAddress(rawURL: string): { origin: string; address: string } {
  const url = new URL(rawURL);
  if (url.protocol !== 'http:' || !isLoopbackHostname(url.hostname)) {
    throw new Error(`Managed development backends require a loopback http URL, got ${rawURL}`);
  }
  if (url.username || url.password || (url.pathname !== '' && url.pathname !== '/') || url.search || url.hash) {
    throw new Error(`Managed development backend URLs cannot contain credentials, a path, query, or fragment: ${rawURL}`);
  }
  const port = url.port || '80';
  const host = url.hostname.includes(':') && !url.hostname.startsWith('[') ? `[${url.hostname}]` : url.hostname;
  return { origin: url.origin, address: `${host}:${port}` };
}

async function healthy(origin: string): Promise<boolean> {
  try {
    const response = await fetch(`${origin}/api/health`, { signal: AbortSignal.timeout(500) });
    return response.ok;
  } catch {
    return false;
  }
}

function childExit(child: ChildProcess): Promise<void> {
  return new Promise((resolve) => child.once('exit', () => resolve()));
}

async function waitUntilHealthy(origin: string, child: ChildProcess, timeoutMs = 15_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let exited: { code: number | null; signal: NodeJS.Signals | null } | undefined;
  child.once('exit', (code, signal) => {
    exited = { code, signal };
  });
  while (Date.now() < deadline) {
    if (exited) {
      throw new Error(`PBVex backend exited before becoming healthy (code ${exited.code ?? 'none'}, signal ${exited.signal ?? 'none'})`);
    }
    if (await healthy(origin)) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`Timed out waiting for the PBVex backend at ${origin}/api/health`);
}

async function stopChild(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return;
  child.kill('SIGTERM');
  const result = await Promise.race([
    childExit(child).then(() => 'exited' as const),
    new Promise<'timeout'>((resolve) => setTimeout(() => resolve('timeout'), 5_000)),
  ]);
  if (result === 'timeout' && child.exitCode === null && child.signalCode === null) {
    child.kill('SIGKILL');
    await childExit(child);
  }
}

export function shouldManageBackend(config: ResolvedConfig): boolean {
  if (config.target !== 'local') return false;
  try {
    const url = new URL(config.url);
    return url.protocol === 'http:' && isLoopbackHostname(url.hostname);
  } catch {
    return false;
  }
}

export async function startManagedBackend(config: ResolvedConfig, options: ManagedBackendOptions = {}): Promise<ManagedBackend> {
  const { origin, address } = localListenAddress(config.url);
  if (await healthy(origin)) {
    throw new Error(
      `A server is already listening at ${origin}. Stop it before running pbvex dev, or use --no-backend with an explicit deployment token.`,
    );
  }

  const dataDir = path.join(config.rootDir, '.pbvex', 'dev', config.target, 'pb_data');
  await mkdir(dataDir, { recursive: true });
  const token = randomBytes(32).toString('base64url');
  const env = { ...process.env, PBVEX_DEV_DEPLOY_TOKEN: token };
  const serverArgs = managedBackendArgs(dataDir, address, options);
  const child = spawnServer(serverArgs, {
    cwd: config.rootDir,
    env,
    stdio: 'inherit',
  });
  child.once('error', (error) => console.error(`PBVex backend process error: ${error.message}`));

  try {
    await waitUntilHealthy(origin, child);
  } catch (error) {
    await stopChild(child);
    throw error;
  }

  console.log(`PBVex backend ready at ${origin}`);
  console.log(`PBVex data: ${dataDir}`);
  if (options.adminUI !== false) console.log(`PocketBase dashboard: ${origin}/_/`);
  return { token, dataDir, close: () => stopChild(child) };
}
