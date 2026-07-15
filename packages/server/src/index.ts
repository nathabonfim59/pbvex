import { accessSync, chmodSync, constants, existsSync, readFileSync } from 'node:fs';
import { spawn, type ChildProcess, type SpawnOptions } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

export interface ServerBinaryManifestEntry {
  file: string;
  goos: string;
  goarch: string;
  goarm?: string;
  sha256: string;
  size: number;
}

export interface ServerBinaryManifest {
  version: 1;
  binaries: Record<string, ServerBinaryManifestEntry>;
}

export interface ResolveServerBinaryOptions {
  platform?: NodeJS.Platform;
  arch?: string;
  env?: NodeJS.ProcessEnv;
}

const packageRoot = fileURLToPath(new URL('..', import.meta.url));

function executableAccessMode(platform: NodeJS.Platform): number {
  return platform === 'win32' ? constants.F_OK : constants.X_OK;
}

export function readServerBinaryManifest(): ServerBinaryManifest {
  const manifestPath = path.join(packageRoot, 'bin', 'manifest.json');
  if (!existsSync(manifestPath)) {
    throw new Error(
      `@pbvex/server is installed without bundled backend binaries. Reinstall PBVex or install @pbvex/server explicitly. Missing: ${manifestPath}`,
    );
  }
  const parsed = JSON.parse(readFileSync(manifestPath, 'utf8')) as ServerBinaryManifest;
  if (parsed.version !== 1 || !parsed.binaries || typeof parsed.binaries !== 'object') {
    throw new Error(`@pbvex/server contains an invalid binary manifest at ${manifestPath}`);
  }
  return parsed;
}

export function resolveServerBinary(options: ResolveServerBinaryOptions = {}): string {
  const platform = options.platform ?? process.platform;
  const arch = options.arch ?? process.arch;
  const env = options.env ?? process.env;
  const override = env.PBVEX_SERVER_BINARY;
  if (override) {
    const resolved = path.resolve(override);
    try {
      accessSync(resolved, executableAccessMode(platform));
    } catch {
      throw new Error(`PBVEX_SERVER_BINARY is not an executable file: ${resolved}`);
    }
    return resolved;
  }

  const manifest = readServerBinaryManifest();
  const key = `${platform}-${arch}`;
  const entry = manifest.binaries[key];
  if (!entry) {
    const supported = Object.keys(manifest.binaries).sort().join(', ');
    throw new Error(
      `@pbvex/server does not include a backend for ${key}. Supported targets: ${supported || 'none'}. ` +
        `Install a PBVex release binary and set PBVEX_SERVER_BINARY to its path.`,
    );
  }

  const binary = path.join(packageRoot, 'bin', entry.file);
  if (platform !== 'win32' && existsSync(binary)) {
    // npm only preserves executable modes for package `bin` entries. The
    // platform binaries are package data, so restore their mode after unpack.
    try {
      chmodSync(binary, 0o755);
    } catch {
      // The access check below reports a useful error for read-only installs.
    }
  }
  try {
    accessSync(binary, executableAccessMode(platform));
  } catch {
    throw new Error(
      `The bundled PBVex backend for ${key} is missing or not executable: ${binary}. ` +
        `Reinstall PBVex or install @pbvex/server explicitly.`,
    );
  }
  return binary;
}

export function spawnServer(
  args: readonly string[],
  options: SpawnOptions & ResolveServerBinaryOptions = {},
): ChildProcess {
  const { platform, arch, env, ...spawnOptions } = options;
  const binary = resolveServerBinary({ platform, arch, env });
  return spawn(binary, [...args], { ...spawnOptions, env: env ?? process.env });
}
