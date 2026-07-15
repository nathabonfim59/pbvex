import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { chmod, mkdir, readFile, rm, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { tmpdir } from 'node:os';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const outputDir = path.join(repoRoot, 'packages', 'server', 'bin');
const key = `${process.platform}-${process.arch}`;
const supported = new Set(['linux-x64', 'linux-arm64', 'linux-arm', 'linux-s390x', 'linux-ppc64', 'darwin-x64', 'darwin-arm64', 'win32-x64', 'win32-arm64']);
if (!supported.has(key)) throw new Error(`Cannot stage a local PBVex backend for unsupported target ${key}`);

for (const entry of await import('node:fs/promises').then(({ readdir }) => readdir(outputDir, { withFileTypes: true }))) {
  if (entry.isDirectory()) await rm(path.join(outputDir, entry.name), { recursive: true, force: true });
}
await rm(path.join(outputDir, 'manifest.json'), { force: true });

const extension = process.platform === 'win32' ? '.exe' : '';
const relative = `${key}/pbvex${extension}`;
const destination = path.join(outputDir, relative);
await mkdir(path.dirname(destination), { recursive: true });
const goTmp = path.join(tmpdir(), 'pbvex-go-tmp');
const goCache = path.join(tmpdir(), 'pbvex-go-cache');
await mkdir(goTmp, { recursive: true });
await mkdir(goCache, { recursive: true });
execFileSync('go', ['build', '-trimpath', '-o', destination, './cmd/pbvex'], {
  cwd: path.join(repoRoot, 'backend'),
  env: { ...process.env, GOTMPDIR: process.env.GOTMPDIR ?? goTmp, GOCACHE: process.env.GOCACHE ?? goCache },
  stdio: 'inherit',
});
if (process.platform !== 'win32') await chmod(destination, 0o755);
const contents = await readFile(destination);
const info = await stat(destination);
const goarch = { x64: 'amd64', arm64: 'arm64', arm: 'arm', s390x: 's390x', ppc64: 'ppc64le' }[process.arch];
const goos = { linux: 'linux', darwin: 'darwin', win32: 'windows' }[process.platform];
const entry = {
  file: relative,
  goos,
  goarch,
  ...(process.arch === 'arm' ? { goarm: '7' } : {}),
  sha256: createHash('sha256').update(contents).digest('hex'),
  size: info.size,
};
await writeFile(path.join(outputDir, 'manifest.json'), `${JSON.stringify({ version: 1, binaries: { [key]: entry } }, null, 2)}\n`);
console.log(`Staged local PBVex server binary for ${key}`);
